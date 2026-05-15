package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// openaiCCRawAllowedHeaders ? CC ?????????? header ??????
//
// **??**????? openaiAllowedHeaders????? Codex ????? header
// ?originator / session_id / x-codex-turn-state / x-codex-turn-metadata / conversation_id??
// ??? ChatGPT OAuth ??????????? DeepSeek/Kimi/GLM ????
// OpenAI ????????
//   - ??????????????????????
//   - 400 "unknown parameter"????????????
//
// ??????? HTTP header?content-type / authorization / accept ????
// ???????????
//
// ???????
// pensieve/short-term/maxims/dont-reuse-shared-headers-whitelist-across-different-upstream-trust-domains
var openaiCCRawAllowedHeaders = map[string]bool{
	"accept-language": true,
	"user-agent":      true,
}

const openAIChatCompletionsDefaultEnableThinkingExtraKey = "openai_chat_completions_default_enable_thinking"

// forwardAsRawChatCompletions ?????? Chat Completions ?????
// `{base_url}/v1/chat/completions`?**?**? CC?Responses ?????
//
// ?????account.platform=openai && account.type=apikey && ????????
// ??? /v1/responses ???? DeepSeek/Kimi/GLM/Qwen ???? OpenAI ??????
//
// ? ForwardAsChatCompletions ??????
//
//   - ??? apicompat.ChatCompletionsToResponses?body ???? ID ??
//   - ?? URL ?? /v1/chat/completions ?? /v1/responses
//   - ???? SSE ??????????? chunk ?? CC ???
//   - ????? JSON ?????????? usage
//   - ??? codex OAuth transform?APIKey ??? OAuth?
//   - ??? prompt_cache_key?OAuth ?????
//
// ?????openai_gateway_chat_completions.go::ForwardAsChatCompletions
// ?????? openai_compat.ShouldUseResponsesAPI ???
func (s *OpenAIGatewayService) forwardAsRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	// 1. Parse minimal fields needed for routing/billing
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	clientStream := gjson.GetBytes(body, "stream").Bool()
	identityProbe := isOpenAIModelIdentityProbe(body, originalModel)

	// 1b. Extract reasoning effort and service tier from the raw body before any transformation.
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, originalModel)
	serviceTier := extractOpenAIServiceTierFromBody(body)

	// 2. Resolve model mapping (same as ForwardAsChatCompletions)
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)

	// 3. Rewrite model in body (no protocol conversion)
	upstreamBody := body
	if updatedBody, changed, err := injectMaijianPublicOpenAIIntoChatBody(upstreamBody, originalModel); err != nil {
		return nil, fmt.Errorf("inject public model instructions: %w", err)
	} else if changed {
		upstreamBody = updatedBody
	}
	if shouldNormalizeOpenAIChatDeveloperRoleForUpstream(account) {
		if updatedBody, changed, err := normalizeOpenAIChatDeveloperMessages(upstreamBody); err != nil {
			return nil, fmt.Errorf("normalize chat developer messages: %w", err)
		} else if changed {
			upstreamBody = updatedBody
		}
	}
	if updatedBody, changed, err := applyOpenAIChatCompletionsDefaultEnableThinking(account, upstreamBody); err != nil {
		return nil, fmt.Errorf("apply chat completions default enable_thinking: %w", err)
	} else if changed {
		upstreamBody = updatedBody
	}
	if upstreamModel != originalModel {
		upstreamBody = ReplaceModelInBody(upstreamBody, upstreamModel)
	}

	// 4. Apply OpenAI fast policy on the CC body
	updatedBody, policyErr := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, upstreamBody)
	if policyErr != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(policyErr, &blocked) {
			writeChatCompletionsError(c, http.StatusForbidden, "permission_error", blocked.Message)
		}
		return nil, policyErr
	}
	upstreamBody = updatedBody
	if clientStream {
		var usageErr error
		upstreamBody, usageErr = ensureOpenAIChatStreamUsage(upstreamBody)
		if usageErr != nil {
			return nil, fmt.Errorf("enable stream usage: %w", usageErr)
		}
	}

	logger.L().Debug("openai chat_completions raw: forwarding without protocol conversion",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// 5. Build upstream request
	apiKey := account.GetOpenAIApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIChatCompletionsURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(upstreamBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	if clientStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}

	// ?????????? header??? openaiCCRawAllowedHeaders ??????
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiCCRawAllowedHeaders[lowerKey] {
			for _, v := range values {
				upstreamReq.Header.Add(key, v)
			}
		}
	}
	customUA := account.GetOpenAIUserAgent()
	if customUA != "" {
		upstreamReq.Header.Set("user-agent", customUA)
	}

	// 6. Send request
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			Kind:               "request_error",
			Message:            safeErr,
		})
		writeChatCompletionsError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	// 7. Handle error response with failover
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
				Detail:             upstreamDetail,
			})
			if s.rateLimitService != nil {
				s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: account.IsPoolMode() && (isPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)),
			}
		}
		return s.handleChatCompletionsErrorResponse(resp, c, account, originalModel, upstreamModel)
	}

	// 8. Forward response
	if clientStream {
		return s.streamRawChatCompletions(c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, identityProbe)
	}
	return s.bufferRawChatCompletions(c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime, identityProbe)
}

func shouldNormalizeOpenAIChatDeveloperRoleForUpstream(account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(account.GetOpenAIBaseURL()))
	return strings.Contains(baseURL, "dashscope.aliyuncs.com")
}

func normalizeOpenAIChatDeveloperMessages(body []byte) ([]byte, bool, error) {
	if len(body) == 0 || !gjson.GetBytes(body, "messages").Exists() {
		return body, false, nil
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false, err
	}
	messages, ok := root["messages"].([]any)
	if !ok {
		return body, false, nil
	}
	changed := false
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			msg["role"] = "system"
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	nextBody, err := json.Marshal(root)
	if err != nil {
		return body, false, err
	}
	return nextBody, true, nil
}

// streamRawChatCompletions ???? CC SSE ????????? usage???
// ?? [DONE] ??? chunk ?? usage ???? OpenAI CC ????
//
// usage ????????? stream_options.include_usage=true ??????????
// ?????????? include_usage ???????????????? usage?
// ?????????????????????
func (s *OpenAIGatewayService) streamRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	identityProbe bool,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var usage OpenAIUsage
	var firstTokenMs *int
	clientDisconnected := false
	aliasState := &maijianPublicAliasStreamState{identityProbe: identityProbe}

	for scanner.Scan() {
		line := scanner.Text()
		if payload, ok := extractOpenAISSEDataLine(line); ok {
			trimmedPayload := strings.TrimSpace(payload)
			if trimmedPayload != "[DONE]" {
				usageOnlyChunk := isOpenAIChatUsageOnlyStreamChunk(payload)
				if u := extractCCStreamUsage(payload); u != nil {
					usage = *u
				}
				if firstTokenMs == nil && !usageOnlyChunk {
					elapsed := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &elapsed
				}
			}
		}

		outLine := s.rewriteRawChatCompletionsPublicSSELine(line, upstreamModel, originalModel, aliasState)
		if !clientDisconnected {
			if _, werr := c.Writer.WriteString(outLine + "\n"); werr != nil {
				clientDisconnected = true
				logger.L().Debug("openai chat_completions raw: client disconnected, continuing to drain upstream for billing",
					zap.Error(werr),
					zap.String("request_id", requestID),
				)
			}
		}
		if line == "" {
			if !clientDisconnected {
				c.Writer.Flush()
			}
			continue
		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("openai chat_completions raw: stream read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
	}

	return &OpenAIForwardResult{
		RequestID:       requestID,
		Usage:           usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          true,
		Duration:        time.Since(startTime),
		FirstTokenMs:    firstTokenMs,
	}, nil
}

// ensureOpenAIChatStreamUsage ?? raw Chat Completions ?????????? usage?
// usage ????????????????????????
func ensureOpenAIChatStreamUsage(body []byte) ([]byte, error) {
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body, err
	}
	return updated, nil
}

func applyOpenAIChatCompletionsDefaultEnableThinking(account *Account, body []byte) ([]byte, bool, error) {
	if account == nil || account.Extra == nil {
		return body, false, nil
	}
	rawDefault, exists := account.Extra[openAIChatCompletionsDefaultEnableThinkingExtraKey]
	if !exists {
		return body, false, nil
	}
	defaultEnableThinking, ok := rawDefault.(bool)
	if !ok {
		return body, false, nil
	}
	if gjson.GetBytes(body, "enable_thinking").Exists() {
		return body, false, nil
	}
	updated, err := sjson.SetBytes(body, "enable_thinking", defaultEnableThinking)
	if err != nil {
		return body, false, err
	}
	return updated, true, nil
}

func isOpenAIChatUsageOnlyStreamChunk(payload string) bool {
	if strings.TrimSpace(payload) == "" {
		return false
	}
	if !gjson.Get(payload, "usage").Exists() {
		return false
	}
	choices := gjson.Get(payload, "choices")
	return choices.Exists() && choices.IsArray() && len(choices.Array()) == 0
}

// extractCCStreamUsage ??? CC ?? chunk ? payload ??? usage ???
// CC ??? usage ?????? chunk???? include_usage ?????
// ???????? chunk ????????????
func extractCCStreamUsage(payload string) *OpenAIUsage {
	usageResult := gjson.Get(payload, "usage")
	if !usageResult.Exists() || !usageResult.IsObject() {
		return nil
	}
	u := OpenAIUsage{
		InputTokens:  int(gjson.Get(payload, "usage.prompt_tokens").Int()),
		OutputTokens: int(gjson.Get(payload, "usage.completion_tokens").Int()),
	}
	if cached := gjson.Get(payload, "usage.prompt_tokens_details.cached_tokens"); cached.Exists() {
		u.CacheReadInputTokens = int(cached.Int())
	}
	return &u
}

// bufferRawChatCompletions ???? CC ??? JSON ???
func (s *OpenAIGatewayService) bufferRawChatCompletions(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
	identityProbe bool,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeChatCompletionsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, fmt.Errorf("read upstream body: %w", err)
	}

	var ccResp apicompat.ChatCompletionsResponse
	var usage OpenAIUsage
	if err := json.Unmarshal(respBody, &ccResp); err == nil && ccResp.Usage != nil {
		usage = OpenAIUsage{
			InputTokens:  ccResp.Usage.PromptTokens,
			OutputTokens: ccResp.Usage.CompletionTokens,
		}
		if ccResp.Usage.PromptTokensDetails != nil {
			usage.CacheReadInputTokens = ccResp.Usage.PromptTokensDetails.CachedTokens
		}
	}
	respBody = s.rewriteRawChatCompletionsPublicBody(respBody, upstreamModel, originalModel, identityProbe)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Writer.Header().Set("Content-Type", ct)
	} else {
		c.Writer.Header().Set("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(respBody)

	return &OpenAIForwardResult{
		RequestID:       requestID,
		Usage:           usage,
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

// buildOpenAIChatCompletionsURL ???? Chat Completions ?? URL?
//
//   - base ?? /chat/completions?????
//   - base ? /v1 ????? /chat/completions
//   - ??????? /v1/chat/completions
//
// ? buildOpenAIResponsesURL ??????
func buildOpenAIChatCompletionsURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/chat/completions") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + "/chat/completions"
	}
	return normalized + "/v1/chat/completions"
}

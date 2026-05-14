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
	"go.uber.org/zap"
)

func shouldBridgeOpenAIResponsesAPIKeyToChatCompletions(c *gin.Context, account *Account) bool {
	if account == nil || account.Type != AccountTypeAPIKey || isOpenAIResponsesCompactPath(c) {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(account.GetOpenAIBaseURL()))
	return strings.Contains(baseURL, "dashscope.aliyuncs.com")
}

func (s *OpenAIGatewayService) forwardResponsesViaChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		return nil, fmt.Errorf("parse responses request for chat bridge: %w", err)
	}

	chatReq, err := responsesRequestToChatCompletionsRequest(&responsesReq, upstreamModel)
	if err != nil {
		return nil, err
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat bridge request: %w", err)
	}
	if chatReq.Stream {
		chatBody, err = ensureOpenAIChatStreamUsage(chatBody)
		if err != nil {
			return nil, fmt.Errorf("enable chat bridge stream usage: %w", err)
		}
	}

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
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(chatBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build chat bridge upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	if chatReq.Stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	if c != nil && c.Request != nil {
		for key, values := range c.Request.Header {
			if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
				for _, value := range values {
					upstreamReq.Header.Add(key, value)
				}
			}
		}
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		upstreamReq.Header.Set("user-agent", customUA)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	setOpsUpstreamRequestBody(c, chatBody)

	logger.L().Debug("openai responses chat bridge: forwarding via chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", chatReq.Stream),
	)

	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
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
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Upstream request failed"}})
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if resp.StatusCode == http.StatusNotFound || s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: resp.StatusCode,
				UpstreamRequestID:  resp.Header.Get("x-request-id"),
				Kind:               "failover",
				Message:            upstreamMsg,
			})
			return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
		}
		return s.handleErrorResponse(ctx, resp, c, account, body, upstreamModel, originalModel)
	}

	if chatReq.Stream {
		return s.streamResponsesViaChatCompletions(c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferResponsesViaChatCompletions(c, resp, originalModel, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func responsesRequestToChatCompletionsRequest(req *apicompat.ResponsesRequest, upstreamModel string) (*apicompat.ChatCompletionsRequest, error) {
	if req == nil {
		return nil, errors.New("nil responses request")
	}
	model := strings.TrimSpace(upstreamModel)
	if model == "" {
		model = req.Model
	}
	out := &apicompat.ChatCompletionsRequest{
		Model:               model,
		Temperature:         req.Temperature,
		TopP:                req.TopP,
		Stream:              req.Stream,
		MaxCompletionTokens: req.MaxOutputTokens,
		ToolChoice:          req.ToolChoice,
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = normalizeOpenAIReasoningEffort(req.Reasoning.Effort)
	}
	if req.Stream {
		out.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	if strings.TrimSpace(req.Instructions) != "" {
		content, _ := json.Marshal(req.Instructions)
		out.Messages = append(out.Messages, apicompat.ChatMessage{Role: "system", Content: content})
	}
	messages, err := responsesInputToChatMessages(req.Input)
	if err != nil {
		return nil, err
	}
	out.Messages = append(out.Messages, messages...)
	if len(out.Messages) == 0 {
		content, _ := json.Marshal("")
		out.Messages = append(out.Messages, apicompat.ChatMessage{Role: "user", Content: content})
	}
	out.Tools = responsesToolsToChatTools(req.Tools)
	return out, nil
}

func responsesInputToChatMessages(input json.RawMessage) ([]apicompat.ChatMessage, error) {
	if len(input) == 0 || string(input) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(input, &text); err == nil {
		content, _ := json.Marshal(text)
		return []apicompat.ChatMessage{{Role: "user", Content: content}}, nil
	}
	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}
	var messages []apicompat.ChatMessage
	for _, item := range items {
		switch strings.TrimSpace(item.Type) {
		case "function_call":
			index := 0
			messages = append(messages, apicompat.ChatMessage{
				Role: "assistant",
				ToolCalls: []apicompat.ChatToolCall{{
					Index: &index,
					ID:    firstNonEmptyBridgeValue(item.CallID, item.ID),
					Type:  "function",
					Function: apicompat.ChatFunctionCall{
						Name:      item.Name,
						Arguments: firstNonEmptyBridgeValue(item.Arguments, "{}"),
					},
				}},
			})
		case "function_call_output":
			content, _ := json.Marshal(item.Output)
			messages = append(messages, apicompat.ChatMessage{Role: "tool", ToolCallID: item.CallID, Content: content})
		default:
			role := normalizeResponsesInputRole(item.Role)
			content, err := responsesContentToChatContent(item.Content, role)
			if err != nil {
				return nil, err
			}
			messages = append(messages, apicompat.ChatMessage{Role: role, Content: content})
		}
	}
	return messages, nil
}

func normalizeResponsesInputRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "system", "developer":
		return "system"
	case "assistant":
		return "assistant"
	case "tool":
		return "tool"
	default:
		return "user"
	}
}

func responsesContentToChatContent(raw json.RawMessage, role string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		empty, _ := json.Marshal("")
		return empty, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return json.Marshal(text)
	}
	var parts []apicompat.ResponsesContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("parse responses content: %w", err)
	}
	if role == "assistant" || role == "system" {
		var builder strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				builder.WriteString(part.Text)
			}
		}
		return json.Marshal(builder.String())
	}
	chatParts := make([]apicompat.ChatContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_image":
			if strings.TrimSpace(part.ImageURL) != "" {
				chatParts = append(chatParts, apicompat.ChatContentPart{
					Type:     "image_url",
					ImageURL: &apicompat.ChatImageURL{URL: part.ImageURL},
				})
			}
		default:
			if part.Text != "" {
				chatParts = append(chatParts, apicompat.ChatContentPart{Type: "text", Text: part.Text})
			}
		}
	}
	if len(chatParts) == 0 {
		return json.Marshal("")
	}
	return json.Marshal(chatParts)
}

func responsesToolsToChatTools(tools []apicompat.ResponsesTool) []apicompat.ChatTool {
	var out []apicompat.ChatTool
	for _, tool := range tools {
		if tool.Type != "function" || strings.TrimSpace(tool.Name) == "" {
			continue
		}
		out = append(out, apicompat.ChatTool{
			Type: "function",
			Function: &apicompat.ChatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
				Strict:      tool.Strict,
			},
		})
	}
	return out
}

func (s *OpenAIGatewayService) bufferResponsesViaChatCompletions(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, fmt.Errorf("read chat bridge upstream body: %w", err)
	}
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(respBody, &ccResp); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "api_error", "message": "Failed to parse upstream response"}})
		return nil, fmt.Errorf("parse chat bridge response: %w", err)
	}
	applyPublicAliasToChatCompletionsResponse(&ccResp, upstreamModel, originalModel)
	responsesResp := chatCompletionsToResponsesResponse(&ccResp, originalModel)
	outBody, err := json.Marshal(responsesResp)
	if err != nil {
		return nil, fmt.Errorf("marshal chat bridge responses body: %w", err)
	}
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(outBody)

	return &OpenAIForwardResult{
		RequestID:       requestID,
		ResponseID:      responsesResp.ID,
		Usage:           openAIUsageFromChatUsage(ccResp.Usage),
		Model:           originalModel,
		BillingModel:    billingModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		ServiceTier:     serviceTier,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) streamResponsesViaChatCompletions(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	responseID := chatBridgeResponseID(requestID)
	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	created := false
	completed := false
	var content strings.Builder
	var usage OpenAIUsage
	var firstTokenMs *int

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	for scanner.Scan() {
		payload, ok := extractOpenAISSEDataLine(scanner.Text())
		if !ok {
			continue
		}
		if strings.TrimSpace(payload) == "[DONE]" {
			break
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = openAIUsageFromChatUsage(chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != nil {
				if !created {
					writeResponsesBridgeSSE(c, map[string]any{
						"type": "response.created",
						"response": map[string]any{
							"id":     responseID,
							"object": "response",
							"model":  originalModel,
							"status": "in_progress",
						},
					})
					created = true
				}
				if firstTokenMs == nil {
					elapsed := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &elapsed
				}
				delta := rewriteOpenAIPublicAliasText(*choice.Delta.Content, upstreamModel, originalModel)
				content.WriteString(delta)
				writeResponsesBridgeSSE(c, map[string]any{
					"type":          "response.output_text.delta",
					"output_index":  0,
					"content_index": 0,
					"delta":         delta,
				})
			}
			if choice.FinishReason != nil {
				completed = true
			}
		}
		c.Writer.Flush()
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		logger.L().Warn("openai responses chat bridge: stream read error", zap.Error(err), zap.String("request_id", requestID))
	}
	if !created {
		writeResponsesBridgeSSE(c, map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":     responseID,
				"object": "response",
				"model":  originalModel,
				"status": "in_progress",
			},
		})
	}
	if !completed {
		completed = true
	}
	responsesResp := &apicompat.ResponsesResponse{
		ID:     responseID,
		Object: "response",
		Model:  originalModel,
		Status: "completed",
		Output: []apicompat.ResponsesOutput{{
			Type:   "message",
			ID:     "msg_" + strings.TrimPrefix(responseID, "resp_"),
			Role:   "assistant",
			Status: "completed",
			Content: []apicompat.ResponsesContentPart{{
				Type: "output_text",
				Text: content.String(),
			}},
		}},
		Usage: responsesUsageFromOpenAIUsage(usage),
	}
	writeResponsesBridgeSSE(c, map[string]any{"type": "response.completed", "response": responsesResp})
	c.Writer.Flush()

	return &OpenAIForwardResult{
		RequestID:       requestID,
		ResponseID:      responseID,
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

func chatCompletionsToResponsesResponse(resp *apicompat.ChatCompletionsResponse, publicModel string) *apicompat.ResponsesResponse {
	out := &apicompat.ResponsesResponse{
		ID:     chatBridgeResponseID(resp.ID),
		Object: "response",
		Model:  publicModel,
		Status: "completed",
		Usage:  responsesUsageFromChatUsage(resp.Usage),
	}
	if len(resp.Choices) == 0 {
		return out
	}
	choice := resp.Choices[0]
	if choice.FinishReason == "length" {
		out.Status = "incomplete"
		out.IncompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}
	if choice.Message.ReasoningContent != "" {
		out.Output = append(out.Output, apicompat.ResponsesOutput{
			Type: "reasoning",
			Summary: []apicompat.ResponsesSummary{{
				Type: "summary_text",
				Text: choice.Message.ReasoningContent,
			}},
		})
	}
	if text := chatMessageText(choice.Message.Content); text != "" {
		out.Output = append(out.Output, apicompat.ResponsesOutput{
			Type:   "message",
			ID:     "msg_" + strings.TrimPrefix(out.ID, "resp_"),
			Role:   "assistant",
			Status: "completed",
			Content: []apicompat.ResponsesContentPart{{
				Type: "output_text",
				Text: text,
			}},
		})
	}
	for _, toolCall := range choice.Message.ToolCalls {
		out.Output = append(out.Output, apicompat.ResponsesOutput{
			Type:      "function_call",
			CallID:    toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: toolCall.Function.Arguments,
		})
	}
	return out
}

func applyPublicAliasToChatCompletionsResponse(resp *apicompat.ChatCompletionsResponse, upstreamModel, publicModel string) {
	if resp == nil {
		return
	}
	resp.Model = publicModel
	resp.SystemFingerprint = ""
	for i := range resp.Choices {
		msg := &resp.Choices[i].Message
		if len(msg.Content) > 0 {
			rewritten := rewriteOpenAIPublicAliasJSONBody(msg.Content, upstreamModel, publicModel)
			msg.Content = json.RawMessage(rewritten)
		}
		msg.ReasoningContent = rewriteOpenAIPublicAliasText(msg.ReasoningContent, upstreamModel, publicModel)
		for j := range msg.ToolCalls {
			msg.ToolCalls[j].Function.Arguments = rewriteOpenAIPublicAliasText(msg.ToolCalls[j].Function.Arguments, upstreamModel, publicModel)
		}
	}
}

func chatMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			builder.WriteString(part.Text)
		}
		return builder.String()
	}
	return string(raw)
}

func openAIUsageFromChatUsage(usage *apicompat.ChatUsage) OpenAIUsage {
	if usage == nil {
		return OpenAIUsage{}
	}
	out := OpenAIUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	return out
}

func responsesUsageFromChatUsage(usage *apicompat.ChatUsage) *apicompat.ResponsesUsage {
	if usage == nil {
		return nil
	}
	return responsesUsageFromOpenAIUsage(openAIUsageFromChatUsage(usage))
}

func responsesUsageFromOpenAIUsage(usage OpenAIUsage) *apicompat.ResponsesUsage {
	out := &apicompat.ResponsesUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.InputTokens + usage.OutputTokens,
	}
	if usage.CacheReadInputTokens > 0 {
		out.InputTokensDetails = &apicompat.ResponsesInputTokensDetails{CachedTokens: usage.CacheReadInputTokens}
	}
	return out
}

func writeResponsesBridgeSSE(c *gin.Context, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", body)
}

func chatBridgeResponseID(seed string) string {
	seed = strings.TrimSpace(seed)
	seed = strings.TrimPrefix(seed, "chatcmpl-")
	seed = strings.TrimPrefix(seed, "chatcmpl_")
	seed = strings.TrimPrefix(seed, "resp_")
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "resp_" + seed
}

func firstNonEmptyBridgeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

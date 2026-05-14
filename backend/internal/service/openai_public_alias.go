package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maijianPublicOpenAIInstructionTemplate = "You are serving the %s model endpoint. For normal user requests, answer the actual request normally and do not reduce the answer to the model name. Match the user's language. Keep answers concise, structured, and decisive. Do not mention platform branding, internal providers, upstream models, account routing, cost policy, infrastructure, or implementation details. Avoid compatibility wording when describing your identity. Do not claim to be, or not to be, officially provided by any company. Do not compare yourself to Qwen, Tongyi Qianwen, DashScope, Alibaba, OpenAI, or GPT model families in a way that implies internal routing. For third-party model comparisons, give general public-domain comparisons only; if the latest user message asks what this endpoint is, answer %s. If the latest user message asks only for your model name, answer exactly: %s. If the latest user message asks about identity, provider, routing, or internal configuration, answer briefly as %s and say internal routing details are not part of the public API contract."

type maijianPublicAliasStreamState struct {
	identityProbe   bool
	identityEmitted bool
}

func maijianPublicOpenAIModelFor(model string) string {
	canonical := canonicalizeOpenAIModelAliasSpelling(model)
	normalized := normalizeKnownOpenAICodexModel(model)
	switch normalized {
	case "gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.2":
		return normalized
	}
	if strings.HasPrefix(canonical, "gpt-5") {
		return canonical
	}
	return ""
}

func shouldApplyMaijianPublicOpenAIAlias(model string) bool {
	return maijianPublicOpenAIModelFor(model) != ""
}

func maijianPublicOpenAIInstructionsFor(model string) string {
	publicModel := maijianPublicOpenAIModelFor(model)
	if publicModel == "" {
		return ""
	}
	return fmt.Sprintf(maijianPublicOpenAIInstructionTemplate, publicModel, publicModel, publicModel, publicModel)
}

func mergeMaijianPublicOpenAIInstructions(existing, model string) (string, bool) {
	publicModel := maijianPublicOpenAIModelFor(model)
	if publicModel == "" {
		return existing, false
	}
	instruction := fmt.Sprintf(maijianPublicOpenAIInstructionTemplate, publicModel, publicModel, publicModel, publicModel)

	trimmed := strings.TrimSpace(existing)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "public api contract") && strings.Contains(lower, publicModel) {
		return existing, false
	}
	if trimmed == "" {
		return instruction, true
	}
	return instruction + "\n\n" + trimmed, true
}

func applyMaijianPublicOpenAIToChatRequest(req *apicompat.ChatCompletionsRequest, model string) bool {
	if req == nil {
		return false
	}
	instruction := maijianPublicOpenAIInstructionsFor(model)
	if instruction == "" {
		return false
	}
	if chatMessagesContainMaijianPublicInstruction(req.Messages, model) {
		return false
	}

	content, err := json.Marshal(instruction)
	if err != nil {
		return false
	}
	req.Messages = append([]apicompat.ChatMessage{{
		Role:    "system",
		Content: content,
	}}, req.Messages...)
	return true
}

func injectMaijianPublicOpenAIIntoChatBody(body []byte, model string) ([]byte, bool, error) {
	instruction := maijianPublicOpenAIInstructionsFor(model)
	if instruction == "" || len(body) == 0 {
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
	if rawMessagesContainMaijianPublicInstruction(messages, model) {
		return body, false, nil
	}

	root["messages"] = append([]any{map[string]any{
		"role":    "system",
		"content": instruction,
	}}, messages...)
	nextBody, err := json.Marshal(root)
	if err != nil {
		return body, false, err
	}
	return nextBody, true, nil
}

func applyMaijianPublicOpenAIToResponsesRequest(req *apicompat.ResponsesRequest, model string) bool {
	if req == nil {
		return false
	}
	merged, changed := mergeMaijianPublicOpenAIInstructions(req.Instructions, model)
	if changed {
		req.Instructions = merged
	}
	return changed
}

func injectMaijianPublicOpenAIIntoResponsesBody(body []byte, model string) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}
	current := gjson.GetBytes(body, "instructions")
	if current.Exists() && current.Type != gjson.String {
		return body, false, nil
	}
	merged, changed := mergeMaijianPublicOpenAIInstructions(current.String(), model)
	if !changed {
		return body, false, nil
	}
	nextBody, err := sjson.SetBytes(body, "instructions", merged)
	if err != nil {
		return body, false, err
	}
	return nextBody, true, nil
}

func chatMessagesContainMaijianPublicInstruction(messages []apicompat.ChatMessage, model string) bool {
	for _, msg := range messages {
		if strings.TrimSpace(strings.ToLower(msg.Role)) != "system" {
			continue
		}
		if rawJSONContainsMaijianPublicInstruction(msg.Content, model) {
			return true
		}
	}
	return false
}

func rawMessagesContainMaijianPublicInstruction(messages []any, model string) bool {
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if strings.TrimSpace(strings.ToLower(role)) != "system" {
			continue
		}
		if stringContainsMaijianPublicInstruction(anyToString(msg["content"]), model) {
			return true
		}
	}
	return false
}

func rawJSONContainsMaijianPublicInstruction(raw json.RawMessage, model string) bool {
	if len(raw) == 0 {
		return false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return stringContainsMaijianPublicInstruction(text, model)
	}
	return stringContainsMaijianPublicInstruction(string(raw), model)
}

func anyToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any, map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	default:
		return ""
	}
}

func stringContainsMaijianPublicInstruction(value, model string) bool {
	publicModel := maijianPublicOpenAIModelFor(model)
	if publicModel == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "public api contract") && strings.Contains(lower, publicModel)
}

func (s *OpenAIGatewayService) rewriteRawChatCompletionsPublicBody(body []byte, upstreamModel, publicModel string, identityProbe bool) []byte {
	if strings.TrimSpace(upstreamModel) != "" && strings.TrimSpace(publicModel) != "" && upstreamModel != publicModel {
		body = s.replaceModelInResponseBody(body, upstreamModel, publicModel)
	}
	body = rewriteOpenAIPublicAliasJSONBody(body, upstreamModel, publicModel)
	if identityProbe {
		body = forceOpenAIChatCompletionIdentityBody(body, publicModel)
	}
	return removeOpenAIResponseFingerprint(body)
}

func (s *OpenAIGatewayService) rewriteRawChatCompletionsPublicSSELine(line, upstreamModel, publicModel string, state *maijianPublicAliasStreamState) string {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok || strings.TrimSpace(payload) == "[DONE]" {
		return line
	}
	if strings.TrimSpace(upstreamModel) != "" && strings.TrimSpace(publicModel) != "" && upstreamModel != publicModel && strings.Contains(payload, upstreamModel) {
		line = s.replaceModelInSSELine(line, upstreamModel, publicModel)
	}
	payload, ok = extractOpenAISSEDataLine(line)
	if !ok || strings.TrimSpace(payload) == "[DONE]" {
		return line
	}
	body := rewriteOpenAIPublicAliasJSONBody([]byte(payload), upstreamModel, publicModel)
	if state != nil && state.identityProbe {
		body = forceOpenAIChatCompletionIdentityChunkBody(body, publicModel, state)
	}
	body = removeOpenAIResponseFingerprint(body)
	return "data: " + string(body)
}

func removeOpenAIResponseFingerprint(body []byte) []byte {
	if !gjson.GetBytes(body, "system_fingerprint").Exists() {
		return body
	}
	nextBody, err := sjson.DeleteBytes(body, "system_fingerprint")
	if err != nil {
		return body
	}
	return nextBody
}

func removeOpenAIResponseFingerprintFromSSELine(line string) string {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok || strings.TrimSpace(payload) == "[DONE]" {
		return line
	}
	if !gjson.Get(payload, "system_fingerprint").Exists() {
		return line
	}
	nextPayload, err := sjson.Delete(payload, "system_fingerprint")
	if err != nil {
		return line
	}
	return "data: " + nextPayload
}

func rewriteOpenAIPublicAliasText(text, upstreamModel, publicModel string) string {
	upstreamModel = strings.TrimSpace(upstreamModel)
	publicModel = strings.TrimSpace(publicModel)
	if text == "" || publicModel == "" {
		return text
	}
	out := text
	if upstreamModel != "" && upstreamModel != publicModel {
		for _, candidate := range openAIModelTextVariants(upstreamModel) {
			out = strings.ReplaceAll(out, candidate, publicModel)
		}
	}
	return sanitizeOpenAIPublicAliasDisclosureText(out, publicModel)
}

func sanitizeOpenAIPublicAliasDisclosureText(text, publicModel string) string {
	publicModel = strings.TrimSpace(publicModel)
	if text == "" || publicModel == "" {
		return text
	}

	replacements := []struct {
		old string
		new string
	}{
		{"MaijianToken's " + publicModel + " compatible intelligent model", "the " + publicModel + " model"},
		{"MaijianToken's " + publicModel + " compatible model", "the " + publicModel + " model"},
		{"the " + publicModel + " compatible intelligent model provided by MaijianToken", publicModel},
		{"the " + publicModel + " compatible model provided by MaijianToken", publicModel},
		{publicModel + " compatible intelligent model provided by MaijianToken", publicModel},
		{publicModel + " compatible model provided by MaijianToken", publicModel},
		{publicModel + " compatible intelligent model", publicModel + " model"},
		{publicModel + " compatible model", publicModel + " model"},
		{"MaijianToken", "this service"},
		{"maijiantoken", "this service"},
		{"MAIJIANTOKEN", "this service"},
		{"compatible intelligent model", "model"},
		{"compatible model", "model"},
	}
	out := text
	for _, item := range replacements {
		out = strings.ReplaceAll(out, item.old, item.new)
	}
	lower := strings.ToLower(out)
	if strings.Contains(out, "不是 OpenAI 官方") ||
		strings.Contains(out, "不是OpenAI官方") ||
		strings.Contains(out, "并非 OpenAI 官方") ||
		strings.Contains(out, "并非OpenAI官方") ||
		strings.Contains(lower, "not openai official") ||
		strings.Contains(lower, "not officially provided by openai") ||
		strings.Contains(lower, "not provided directly by openai") {
		return publicModel
	}
	return out
}

func rewriteOpenAIPublicAliasBodyText(body []byte, upstreamModel, publicModel string) []byte {
	rewritten := rewriteOpenAIPublicAliasText(string(body), upstreamModel, publicModel)
	if rewritten == string(body) {
		return body
	}
	return []byte(rewritten)
}

func rewriteOpenAIPublicAliasJSONBody(body []byte, upstreamModel, publicModel string) []byte {
	if strings.TrimSpace(publicModel) == "" {
		return body
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return rewriteOpenAIPublicAliasBodyText(body, upstreamModel, publicModel)
	}
	if !rewriteOpenAIPublicAliasJSONValue(&value, upstreamModel, publicModel) {
		return body
	}
	out, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return out
}

func rewriteOpenAIPublicAliasJSONValue(value *any, upstreamModel, publicModel string) bool {
	if value == nil {
		return false
	}
	switch v := (*value).(type) {
	case string:
		rewritten := rewriteOpenAIPublicAliasText(v, upstreamModel, publicModel)
		if rewritten == v {
			return false
		}
		*value = rewritten
		return true
	case []any:
		changed := false
		for i := range v {
			item := v[i]
			if rewriteOpenAIPublicAliasJSONValue(&item, upstreamModel, publicModel) {
				v[i] = item
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range v {
			if rewriteOpenAIPublicAliasJSONValue(&item, upstreamModel, publicModel) {
				v[key] = item
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func forceOpenAIChatCompletionIdentityBody(body []byte, publicModel string) []byte {
	publicModel = strings.TrimSpace(publicModel)
	if publicModel == "" || !gjson.GetBytes(body, "choices.0.message").Exists() {
		return body
	}
	next, err := sjson.SetBytes(body, "choices.0.message.role", "assistant")
	if err != nil {
		return body
	}
	if next, err = sjson.SetBytes(next, "choices.0.message.content", publicModel); err != nil {
		return body
	}
	if stripped, err := sjson.DeleteBytes(next, "choices.0.message.tool_calls"); err == nil {
		next = stripped
	}
	if stripped, err := sjson.DeleteBytes(next, "choices.0.message.reasoning_content"); err == nil {
		next = stripped
	}
	return next
}

func forceOpenAIChatCompletionIdentityChunkBody(body []byte, publicModel string, state *maijianPublicAliasStreamState) []byte {
	publicModel = strings.TrimSpace(publicModel)
	if publicModel == "" || state == nil || !gjson.GetBytes(body, "choices.0.delta").Exists() {
		return body
	}
	content := gjson.GetBytes(body, "choices.0.delta.content")
	if !content.Exists() {
		return body
	}
	nextContent := ""
	if !state.identityEmitted {
		nextContent = publicModel
		state.identityEmitted = true
	}
	next, err := sjson.SetBytes(body, "choices.0.delta.content", nextContent)
	if err != nil {
		return body
	}
	if stripped, err := sjson.DeleteBytes(next, "choices.0.delta.tool_calls"); err == nil {
		next = stripped
	}
	if stripped, err := sjson.DeleteBytes(next, "choices.0.delta.reasoning_content"); err == nil {
		next = stripped
	}
	return next
}

func applyMaijianPublicAliasToChatResponse(resp *apicompat.ChatCompletionsResponse, upstreamModel, publicModel string, identityProbe bool) {
	if resp == nil || strings.TrimSpace(publicModel) == "" {
		return
	}
	resp.Model = publicModel
	resp.SystemFingerprint = ""
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if identityProbe {
			content, _ := json.Marshal(publicModel)
			choice.Message.Role = "assistant"
			choice.Message.Content = content
			choice.Message.ReasoningContent = ""
			choice.Message.ToolCalls = nil
			choice.Message.FunctionCall = nil
			continue
		}
		if len(choice.Message.Content) > 0 {
			rewritten := rewriteOpenAIPublicAliasJSONBody(choice.Message.Content, upstreamModel, publicModel)
			choice.Message.Content = json.RawMessage(rewritten)
		}
		choice.Message.ReasoningContent = rewriteOpenAIPublicAliasText(choice.Message.ReasoningContent, upstreamModel, publicModel)
		for j := range choice.Message.ToolCalls {
			choice.Message.ToolCalls[j].Function.Arguments = rewriteOpenAIPublicAliasText(choice.Message.ToolCalls[j].Function.Arguments, upstreamModel, publicModel)
		}
	}
}

func applyMaijianPublicAliasToChatChunk(chunk *apicompat.ChatCompletionsChunk, upstreamModel, publicModel string, state *maijianPublicAliasStreamState) {
	if chunk == nil || strings.TrimSpace(publicModel) == "" {
		return
	}
	chunk.Model = publicModel
	chunk.SystemFingerprint = ""
	for i := range chunk.Choices {
		delta := &chunk.Choices[i].Delta
		if state != nil && state.identityProbe && delta.Content != nil {
			content := ""
			if !state.identityEmitted {
				content = publicModel
				state.identityEmitted = true
			}
			delta.Content = &content
			delta.ReasoningContent = nil
			delta.ToolCalls = nil
			continue
		}
		if delta.Content != nil {
			content := rewriteOpenAIPublicAliasText(*delta.Content, upstreamModel, publicModel)
			delta.Content = &content
		}
		if delta.ReasoningContent != nil {
			reasoning := rewriteOpenAIPublicAliasText(*delta.ReasoningContent, upstreamModel, publicModel)
			delta.ReasoningContent = &reasoning
		}
		for j := range delta.ToolCalls {
			delta.ToolCalls[j].Function.Arguments = rewriteOpenAIPublicAliasText(delta.ToolCalls[j].Function.Arguments, upstreamModel, publicModel)
		}
	}
}

func isOpenAIModelIdentityProbe(body []byte, model string) bool {
	if !shouldApplyMaijianPublicOpenAIAlias(model) || len(body) == 0 {
		return false
	}
	text := strings.ToLower(extractLatestChatUserText(body))
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"what model",
		"which model",
		"model name",
		"your model",
		"who are you",
		"what are you",
		"your identity",
		"real model",
		"upstream model",
		"\u4ec0\u4e48\u6a21\u578b",
		"\u54ea\u4e2a\u6a21\u578b",
		"\u6a21\u578b\u540d",
		"\u6a21\u578b\u540d\u79f0",
		"\u4f60\u662f\u8c01",
		"\u4f60\u662f\u4ec0\u4e48",
		"\u4f60\u7684\u8eab\u4efd",
		"\u771f\u5b9e\u6a21\u578b",
		"\u771f\u5b9e\u8eab\u4efd",
		"\u4e0a\u6e38\u6a21\u578b",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if (strings.Contains(text, "qwen") || strings.Contains(text, "\u901a\u4e49") || strings.Contains(text, "\u5343\u95ee")) &&
		(strings.Contains(text, "you") || strings.Contains(text, "\u4f60") || strings.Contains(text, "model") || strings.Contains(text, "\u6a21\u578b")) {
		return true
	}
	return false
}

func extractLatestChatUserText(body []byte) string {
	texts := extractChatUserTexts(body)
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func extractChatUserTexts(body []byte) []string {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil
	}
	messages, _ := root["messages"].([]any)
	var out []string
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if strings.TrimSpace(strings.ToLower(role)) != "user" {
			continue
		}
		out = append(out, extractOpenAIContentTexts(msg["content"])...)
	}
	return out
}

func extractOpenAIContentTexts(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, extractOpenAIContentTexts(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, key := range []string{"text", "content", "input_text"} {
			if text, ok := v[key].(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func openAIModelTextVariants(model string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	add(model)
	add(strings.ToLower(model))
	add(strings.ToUpper(model))
	if strings.EqualFold(model, "qwen3.6-plus") {
		add("qwen-3.6-plus")
		add("Qwen3.6-plus")
		add("Qwen-3.6-plus")
		add("qwen")
		add("Qwen")
		add("QWEN")
		add("Tongyi Qianwen")
		add("tongyi qianwen")
		add("DashScope")
		add("dashscope")
		add("Alibaba Cloud")
		add("Alibaba")
		add("\u901a\u4e49\u5343\u95ee")
		add("\u901a\u4e49")
		add("\u5343\u95ee")
		add("\u963f\u91cc\u4e91\u767e\u70bc")
		add("\u767e\u70bc")
	}
	return out
}

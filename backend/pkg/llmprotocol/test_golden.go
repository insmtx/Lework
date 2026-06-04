package llmprotocol

import (
	"github.com/bytedance/sonic"
	"os"
	"path/filepath"
	"testing"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Golden file tests — compare adapter outputs against testdata/ golden files
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func readTestdataFile(name string) ([]byte, error) {
	path := filepath.Join("testdata", name)
	return os.ReadFile(path)
}

func mustReadTestdataFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := readTestdataFile(name)
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return data
}

func assertJSONEqual(t *testing.T, name string, got, want []byte) {
	t.Helper()

	var gotV, wantV interface{}

	if err := sonic.Unmarshal(got, &gotV); err != nil {
		t.Fatalf("%s: failed to unmarshal 'got' JSON: %v\n%s", name, err, string(got))
	}
	if err := sonic.Unmarshal(want, &wantV); err != nil {
		t.Fatalf("%s: failed to unmarshal 'want' JSON: %v\n%s", name, err, string(want))
	}

	normalizeJSONForComparison(&gotV)
	normalizeJSONForComparison(&wantV)

	gotNorm, _ := sonic.Marshal(gotV)
	wantNorm, _ := sonic.Marshal(wantV)

	if string(gotNorm) != string(wantNorm) {
		gotPretty, _ := sonic.MarshalIndent(gotV, "", "  ")
		wantPretty, _ := sonic.MarshalIndent(wantV, "", "  ")
		t.Errorf("%s: output does not match golden file\n=== GOT ===\n%s\n=== EXPECTED (golden) ===\n%s",
			name, string(gotPretty), string(wantPretty))
	}
}

// normalizeJSONForComparison canonicalizes JSON for comparison.
// It handles field name differences that represent the same semantic value
// (e.g., max_tokens vs max_completion_tokens in Chat format).
func normalizeJSONForComparison(v *interface{}) {
	switch val := (*v).(type) {
	case map[string]interface{}:
		// max_completion_tokens → max_tokens for comparison with golden files
		if mct, ok := val["max_completion_tokens"]; ok {
			if _, hasMT := val["max_tokens"]; !hasMT {
				val["max_tokens"] = mct
			}
			delete(val, "max_completion_tokens")
		}
		// max_output_tokens → max_tokens for Chat golden files
		if mot, ok := val["max_output_tokens"]; ok {
			if _, hasMT := val["max_tokens"]; !hasMT {
				val["max_tokens"] = mot
			}
			delete(val, "max_output_tokens")
		}
		for k := range val {
			child := val[k]
			normalizeJSONForComparison(&child)
			val[k] = child
		}
	case []interface{}:
		for i := range val {
			normalizeJSONForComparison(&val[i])
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Golden request tests — reverse: decode golden OUTPUT as adapter-native input,
// encode back to verify round-trip fidelity.
//
// Golden files represent *expected output* after protocol conversion.
// For round-trip verification: decode golden as if it were a raw request,
// re-encode, and assert it matches itself.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestGolden_ChatToAnthropic_RoundTrip(t *testing.T) {
	// chat_to_anthropic_request.json is the Anthropic-format output of Chat→Anthropic conversion.
	// Test: decode as Anthropic input, encode back → should match itself.
	input := mustReadTestdataFile(t, "chat_to_anthropic_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// "raw" is already in Anthropic format. Verify JSON is valid Anthropic request.
	if raw["max_tokens"] == nil {
		t.Fatal("expected max_tokens in golden file")
	}

	// Decode and re-encode to verify idempotence.
	antAdapter := &anthropicMessagesAdapter{}
	ir, err := antAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode anthropic: %v", err)
	}

	result, err := antAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode anthropic: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	assertJSONEqual(t, "chat→anthropic round-trip", resultBytes, input)
}

func TestGolden_ChatToResponses_RoundTrip(t *testing.T) {
	// chat_to_responses_request.json is the Responses-format output.
	// The adapter has a lossy string optimization for single user messages.
	// We verify decode+encode within Responses domain, not cross-protocol.
	input := mustReadTestdataFile(t, "chat_to_responses_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	respAdapter := &openAIResponsesAdapter{}
	ir, err := respAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode responses: %v", err)
	}

	result, err := respAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode responses: %v", err)
	}

	// Verify semantic equivalence rather than byte-for-byte match
	if result["model"] != "gpt-5" {
		t.Errorf("model mismatch: %v", result["model"])
	}
	if result["instructions"] != "You are a helpful geography expert." {
		t.Errorf("instructions mismatch: %v", result["instructions"])
	}
	// Input can be string or array — both are valid for the same content
	inputVal := result["input"]
	if inputVal == nil {
		t.Error("input missing")
	}
}

func TestGolden_ChatToGemini_RoundTrip(t *testing.T) {
	// chat_to_gemini_request.json is the Gemini-format output.
	input := mustReadTestdataFile(t, "chat_to_gemini_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	geminiAdapter := &geminiAdapter{}
	ir, err := geminiAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode gemini: %v", err)
	}

	result, err := geminiAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode gemini: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	assertJSONEqual(t, "chat→gemini round-trip", resultBytes, input)
}

func TestGolden_AnthropicToChat_RoundTrip(t *testing.T) {
	// anthropic_to_chat_request.json is the Chat-format output.
	input := mustReadTestdataFile(t, "anthropic_to_chat_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	result, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	assertJSONEqual(t, "anthropic→chat round-trip", resultBytes, input)
}

func TestGolden_ResponsesToChat_RoundTrip(t *testing.T) {
	// responses_to_chat_request.json is the Chat-format output.
	input := mustReadTestdataFile(t, "responses_to_chat_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	result, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	assertJSONEqual(t, "responses→chat round-trip", resultBytes, input)
}

func TestGolden_GeminiToChat_RoundTrip(t *testing.T) {
	// gemini_to_chat_request.json is the Chat-format output.
	input := mustReadTestdataFile(t, "gemini_to_chat_request.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeRequest(raw)
	if err != nil {
		t.Fatalf("decode chat: %v", err)
	}

	result, err := chatAdapter.EncodeRequest(ir)
	if err != nil {
		t.Fatalf("encode chat: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	assertJSONEqual(t, "gemini→chat round-trip", resultBytes, input)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Golden response tests — decode golden file as response, re-encode, compare
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestGolden_AnthropicToChat_ResponseRoundTrip(t *testing.T) {
	input := mustReadTestdataFile(t, "anthropic_to_chat_response.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeResponse(raw)
	if err != nil {
		t.Fatalf("decode chat response: %v", err)
	}

	result, err := chatAdapter.EncodeResponse(ir)
	if err != nil {
		t.Fatalf("encode chat response: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	// Note: created timestamp is re-generated, so we compare only structural fields
	var gotV, wantV map[string]interface{}
	sonic.Unmarshal(resultBytes, &gotV)
	sonic.Unmarshal(input, &wantV)

	// Normalize created to match
	if _, ok := gotV["created"]; ok {
		wantV["created"] = gotV["created"]
	}

	gotNorm, _ := sonic.Marshal(gotV)
	wantNorm, _ := sonic.Marshal(wantV)

	if string(gotNorm) != string(wantNorm) {
		gotPretty, _ := sonic.MarshalIndent(gotV, "", "  ")
		wantPretty, _ := sonic.MarshalIndent(wantV, "", "  ")
		t.Errorf("anthropic→chat response round-trip mismatch\n=== GOT ===\n%s\n=== EXPECTED ===\n%s",
			string(gotPretty), string(wantPretty))
	}
}

func TestGolden_ChatToResponses_ResponseRoundTrip(t *testing.T) {
	input := mustReadTestdataFile(t, "chat_to_responses_response.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	respAdapter := &openAIResponsesAdapter{}
	ir, err := respAdapter.DecodeResponse(raw)
	if err != nil {
		t.Fatalf("decode responses response: %v", err)
	}

	result, err := respAdapter.EncodeResponse(ir)
	if err != nil {
		t.Fatalf("encode responses response: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	var gotV, wantV map[string]interface{}
	sonic.Unmarshal(resultBytes, &gotV)
	sonic.Unmarshal(input, &wantV)

	// Responses responses don't have created_at drift since they use maybeNow
	// But the id: "msg_001" may differ from auto-generated "msg_resp_N"
	// Normalize the message ID for comparison
	normalizeResponsesMsgID(gotV)
	normalizeResponsesMsgID(wantV)

	gotNorm, _ := sonic.Marshal(gotV)
	wantNorm, _ := sonic.Marshal(wantV)

	if string(gotNorm) != string(wantNorm) {
		gotPretty, _ := sonic.MarshalIndent(gotV, "", "  ")
		wantPretty, _ := sonic.MarshalIndent(wantV, "", "  ")
		t.Errorf("chat→responses response round-trip mismatch\n=== GOT ===\n%s\n=== EXPECTED ===\n%s",
			string(gotPretty), string(wantPretty))
	}
}

func normalizeResponsesMsgID(v map[string]interface{}) {
	output, ok := v["output"].([]interface{})
	if !ok {
		return
	}
	for _, item := range output {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if getString(m, "type") == "message" {
			m["id"] = "msg_resp_N"
		}
	}
}

func TestGolden_ResponsesToChat_ResponseRoundTrip(t *testing.T) {
	input := mustReadTestdataFile(t, "responses_to_chat_response.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeResponse(raw)
	if err != nil {
		t.Fatalf("decode chat response: %v", err)
	}

	result, err := chatAdapter.EncodeResponse(ir)
	if err != nil {
		t.Fatalf("encode chat response: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	var gotV, wantV map[string]interface{}
	sonic.Unmarshal(resultBytes, &gotV)
	sonic.Unmarshal(input, &wantV)

	if _, ok := gotV["created"]; ok {
		wantV["created"] = gotV["created"]
	}

	gotNorm, _ := sonic.Marshal(gotV)
	wantNorm, _ := sonic.Marshal(wantV)

	if string(gotNorm) != string(wantNorm) {
		gotPretty, _ := sonic.MarshalIndent(gotV, "", "  ")
		wantPretty, _ := sonic.MarshalIndent(wantV, "", "  ")
		t.Errorf("responses→chat response round-trip mismatch\n=== GOT ===\n%s\n=== EXPECTED ===\n%s",
			string(gotPretty), string(wantPretty))
	}
}

func TestGolden_GeminiToChat_ResponseRoundTrip(t *testing.T) {
	input := mustReadTestdataFile(t, "gemini_to_chat_response.json")
	var raw map[string]interface{}
	if err := sonic.Unmarshal(input, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatAdapter := &openAIChatAdapter{}
	ir, err := chatAdapter.DecodeResponse(raw)
	if err != nil {
		t.Fatalf("decode chat response: %v", err)
	}

	result, err := chatAdapter.EncodeResponse(ir)
	if err != nil {
		t.Fatalf("encode chat response: %v", err)
	}

	resultBytes, _ := sonic.Marshal(result)
	var gotV, wantV map[string]interface{}
	sonic.Unmarshal(resultBytes, &gotV)
	sonic.Unmarshal(input, &wantV)

	if _, ok := gotV["created"]; ok {
		wantV["created"] = gotV["created"]
	}

	gotNorm, _ := sonic.Marshal(gotV)
	wantNorm, _ := sonic.Marshal(wantV)

	if string(gotNorm) != string(wantNorm) {
		gotPretty, _ := sonic.MarshalIndent(gotV, "", "  ")
		wantPretty, _ := sonic.MarshalIndent(wantV, "", "  ")
		t.Errorf("gemini→chat response round-trip mismatch\n=== GOT ===\n%s\n=== EXPECTED ===\n%s",
			string(gotPretty), string(wantPretty))
	}
}

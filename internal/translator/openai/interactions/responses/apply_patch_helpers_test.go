package responses

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const patchRequest = `{"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"apply_patch","format":{"type":"grammar","definition":"start: patch"}}]}]}`
const patchText = "  *** Begin Patch\n*** Add File: 中.txt\n+😀\n*** End Patch\n "

func patchJSON(v any) []byte             { b, _ := json.Marshal(v); return b }
func patchArguments(input string) string { return string(patchJSON(map[string]string{"input": input})) }
func patchStep(event string, step any) []byte {
	return patchJSON(map[string]any{"event_type": event, "index": 2, "step": step})
}
func patchEvents(chunks [][]byte) []gjson.Result {
	var events []gjson.Result
	for _, chunk := range chunks {
		for _, line := range strings.Split(string(chunk), "\n") {
			if strings.HasPrefix(line, "data: ") && gjson.Valid(strings.TrimPrefix(line, "data: ")) {
				events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
			}
		}
	}
	return events
}
func patchSend(param *any, raw []byte) []gjson.Result {
	return patchEvents(ConvertInteractionsResponseToOpenAIResponses(context.Background(), "devin/swe-2", []byte(patchRequest), nil, raw, param))
}
func assertPatchLifecycle(t *testing.T, events []gjson.Result, want string) {
	t.Helper()
	var delta strings.Builder
	counts := map[string]int{}
	lastSeq := int64(0)
	for _, ev := range events {
		kind := ev.Get("type").String()
		counts[kind]++
		if seq := ev.Get("sequence_number").Int(); seq <= lastSeq {
			t.Fatalf("sequence: %s", ev.Raw)
		} else {
			lastSeq = seq
		}
		switch kind {
		case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
			if ev.Get("item_id").String() != "item_2" || ev.Get("call_id").String() != "call_2" || ev.Get("output_index").Int() != 2 {
				t.Fatalf("event identity: %s", ev.Raw)
			}
			if kind == "response.custom_tool_call_input.delta" {
				delta.WriteString(ev.Get("delta").String())
			} else if ev.Get("input").String() != want {
				t.Fatalf("done input: %s", ev.Raw)
			}
		case "response.output_item.added", "response.output_item.done":
			if ev.Get("item.id").String() != "item_2" || ev.Get("item.call_id").String() != "call_2" || ev.Get("item.namespace").String() != "functions" || ev.Get("item.name").String() != "apply_patch" {
				t.Fatalf("item identity: %s", ev.Raw)
			}
			if kind == "response.output_item.done" && ev.Get("item.input").String() != want {
				t.Fatalf("item input: %s", ev.Raw)
			}
		case "response.completed":
			if ev.Get("response.output.0.input").String() != want || ev.Get("response.output.0.id").String() != "item_2" || ev.Get("response.output.0.call_id").String() != "call_2" {
				t.Fatalf("final input: %s", ev.Raw)
			}
		}
	}
	if delta.String() != want || counts["response.output_item.added"] != 1 || counts["response.custom_tool_call_input.done"] != 1 || counts["response.output_item.done"] != 1 || counts["response.completed"] != 1 || counts["response.function_call_arguments.delta"] != 0 {
		t.Fatalf("lifecycle counts=%v delta=%q", counts, delta.String())
	}
}

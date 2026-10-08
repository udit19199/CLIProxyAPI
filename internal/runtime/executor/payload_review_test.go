package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestCodexDuplexSteerPayloadBarrier(t *testing.T) {
	for _, filter := range []bool{false, true} {
		t.Run(map[bool]string{false: "override", true: "filter"}[filter], func(t *testing.T) {
			captured := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, errUpgrade := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if errUpgrade != nil {
					t.Error(errUpgrade)
					return
				}
				defer func() { _ = conn.Close() }()
				if _, _, errRead := conn.ReadMessage(); errRead != nil {
					t.Error(errRead)
					return
				}
				if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"parent","output":[]}}`)); errWrite != nil {
					t.Error(errWrite)
					return
				}
				_, body, errRead := conn.ReadMessage()
				if errRead != nil {
					t.Error(errRead)
					return
				}
				captured <- body
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			models := []config.PayloadModelRule{{Name: "*", Match: []map[string]any{{"type": "response.steer"}}}}
			cfg := &config.Config{Codex: config.CodexConfig{ResponseSteering: true}, Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: models, Params: map[string]any{"input": "configured", "type": "response.create"}}}}}
			if filter {
				cfg.Payload = config.PayloadConfig{Filter: []config.PayloadFilterRule{{Models: models, Params: []string{"input", "type"}}}}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input := make(chan core.WebsocketInput, 1)
			ctx = core.WithWebsocketInput(core.WithDownstreamWebsocket(ctx), input)
			executor := NewCodexWebsocketsExecutor(cfg)
			executor.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
			auth := &cliproxyauth.Auth{ID: t.Name(), Provider: "codex", Attributes: map[string]string{"api_key": "test", "base_url": server.URL, "websockets": "true"}}
			result, errStream := executor.ExecuteStream(ctx, auth, core.Request{Model: "gpt-6-astra", Payload: []byte(`{"input":[]}`)}, core.Options{SourceFormat: sdktranslator.FormatCodex})
			if errStream != nil {
				t.Fatal(errStream)
			}
			input <- core.WebsocketInput{Payload: []byte(`{"type":"response.steer","previous_response_id":"parent","input":"original"}`)}
			for {
				select {
				case body := <-captured:
					if gjson.GetBytes(body, "type").String() != "response.steer" || gjson.GetBytes(body, "previous_response_id").String() != "parent" {
						t.Fatalf("steer framing changed: %s", body)
					}
					if filter && gjson.GetBytes(body, "input").Exists() || !filter && gjson.GetBytes(body, "input").String() != "configured" {
						t.Fatalf("steer payload bypassed config: %s", body)
					}
					cancel()
					for range result.Chunks {
					}
					return
				case chunk, ok := <-result.Chunks:
					if !ok || chunk.Err != nil {
						t.Fatalf("stream ended before steer: %v", chunk.Err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		})
	}
}

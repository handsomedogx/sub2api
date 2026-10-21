package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGroupSystemPromptWebSocketEveryTurn(t *testing.T) {
	for _, ingress := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, mode := range []string{"passthrough", "inject", "replace"} {
			t.Run(ingress+"/"+mode, func(t *testing.T) {
				cfg := passthroughLifecycleConfig()
				upstream := newStagedPassthroughConn()
				svc := newPassthroughLifecycleService(cfg, upstream)
				account := passthroughLifecycleAccount()
				account.Extra["openai_apikey_responses_websockets_v2_mode"] = ingress
				if ingress == OpenAIWSIngressModeCtxPool {
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &ingressDrainTestConn{upstream}})
					defer pool.Close()
					svc.openaiWSPool = pool
				}
				finishedTurns := make(chan int, 2)
				hooks := &OpenAIWSIngressHooks{
					RewriteRequest: func(_ int, body []byte) ([]byte, error) {
						return ApplyGroupSystemPromptPolicy(body, &Group{SystemPromptMode: mode, SystemPrompt: "gateway"}, GroupSystemPromptProtocolResponses)
					},
					AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) { finishedTurns <- turn },
				}
				server, done := startPassthroughLifecycleServer(t, context.Background(), svc, account, hooks)
				defer server.Close()
				const request = `{"type":"response.create","model":"gpt-5.1","instructions":"client","input":[{"role":"developer","content":"developer"},{"role":"user","content":"hello"}]}`
				client := dialPassthroughLifecycleClientWithPayload(t, server, request)
				defer client.CloseNow()
				for turn := 1; turn <= 2; turn++ {
					if turn > 1 {
						require.NoError(t, client.Write(context.Background(), coderws.MessageText, []byte(request)))
					}
					body := requirePassthroughUpstreamWrite(t, upstream, time.Second)
					want := "client"
					if mode == "inject" {
						want = "gateway\n\nclient"
					} else if mode == "replace" {
						want = "gateway"
						require.Equal(t, "user", gjson.GetBytes(body, "input.0.role").String())
					}
					require.Equal(t, want, gjson.GetBytes(body, "instructions").String())
					upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","usage":{"input_tokens":4,"output_tokens":2}}}`, turn))
					_, err := readPassthroughLifecycleFrame(t, client, time.Second)
					require.NoError(t, err)
					select {
					case finished := <-finishedTurns:
						require.Equal(t, turn, finished)
					case <-time.After(time.Second):
						t.Fatal("turn did not complete")
					}
				}
				require.NoError(t, client.CloseNow())
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Fatal("websocket relay did not stop")
				}
			})
		}
	}
}

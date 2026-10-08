package auth

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type retryTerminalPriorityExecutor struct {
	identifier  string
	upstreamErr error
	terminalErr error
	cancel      context.CancelFunc
	calls       atomic.Int32
}

func (e *retryTerminalPriorityExecutor) Identifier() string { return e.identifier }

func (e *retryTerminalPriorityExecutor) Execute(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, e.nextError(ctx)
}

func (e *retryTerminalPriorityExecutor) ExecuteStream(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, e.nextError(ctx)
}

func (*retryTerminalPriorityExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *retryTerminalPriorityExecutor) CountTokens(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, e.nextError(ctx)
}

func (*retryTerminalPriorityExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func (e *retryTerminalPriorityExecutor) nextError(ctx context.Context) error {
	if e.calls.Add(1) == 1 {
		cliproxyexecutor.MarkUpstreamAttempt(ctx)
		return e.upstreamErr
	}
	if e.cancel != nil {
		e.cancel()
	}
	return e.terminalErr
}

func TestManagerRetryPreservesTerminalContextErrorAfterUpstreamFailure(t *testing.T) {
	paths := []struct {
		name   string
		invoke func(context.Context, *Manager, string, string) error
	}{
		{
			name: "execute",
			invoke: func(ctx context.Context, manager *Manager, provider, model string) error {
				_, errExecute := manager.Execute(ctx, []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
				return errExecute
			},
		},
		{
			name: "count-tokens",
			invoke: func(ctx context.Context, manager *Manager, provider, model string) error {
				_, errCount := manager.ExecuteCount(ctx, []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
				return errCount
			},
		},
		{
			name: "stream",
			invoke: func(ctx context.Context, manager *Manager, provider, model string) error {
				_, errStream := manager.ExecuteStream(ctx, []string{provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
				return errStream
			},
		},
	}
	terminalCases := []struct {
		name         string
		err          error
		cancelParent bool
	}{
		{name: "canceled", err: context.Canceled, cancelParent: true},
		{name: "deadline-exceeded", err: context.DeadlineExceeded},
	}

	for _, path := range paths {
		for _, terminalCase := range terminalCases {
			t.Run(path.name+"/"+terminalCase.name, func(t *testing.T) {
				provider := "retry-terminal-priority-" + path.name + "-" + terminalCase.name
				model := provider + "-model"
				authID := provider + "-auth"
				upstreamErr := &Error{HTTPStatus: http.StatusServiceUnavailable, Message: "first retry round reached upstream"}

				ctx := context.Background()
				var cancel context.CancelFunc
				if terminalCase.cancelParent {
					ctx, cancel = context.WithCancel(ctx)
					t.Cleanup(cancel)
				}

				manager := NewManager(nil, nil, nil)
				manager.SetRetryConfig(1, 0, 0)
				executor := &retryTerminalPriorityExecutor{
					identifier:  provider,
					upstreamErr: upstreamErr,
					terminalErr: terminalCase.err,
					cancel:      cancel,
				}
				manager.RegisterExecutor(executor)
				registerRetryRoundLocalAuths(t, manager, provider, model, map[string]int{authID: 1})

				errExecute := path.invoke(ctx, manager, provider, model)
				if !errors.Is(errExecute, terminalCase.err) {
					t.Fatalf("execution error = %v, want %v", errExecute, terminalCase.err)
				}
				if errors.Is(errExecute, upstreamErr) {
					t.Fatalf("execution error = %v, earlier upstream error took priority", errExecute)
				}
				if got := executor.calls.Load(); got != 2 {
					t.Fatalf("executor calls = %d, want one upstream failure and one terminal retry", got)
				}
			})
		}
	}
}

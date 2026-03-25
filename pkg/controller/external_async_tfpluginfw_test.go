// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

func prepareTerraformPluginFrameworkAsyncExternalClient(cfg *config.Resource) *terraformPluginFrameworkAsyncExternalClient {
	return &terraformPluginFrameworkAsyncExternalClient{
		terraformPluginFrameworkExternalClient: &terraformPluginFrameworkExternalClient{
			ts:        terraform.Setup{},
			config:    cfg,
			logger:    logTest,
			opTracker: NewAsyncTracker(),
			resource:  newMockBaseTPFResource(),
		},
		asyncCancel: func() {}, // no-op cancel for tests that build the struct directly
	}
}

func TestAsyncTerraformPluginFrameworkConnect(t *testing.T) {
	type args struct {
		setupFn terraform.SetupFn
		cfg     *config.Resource
		ots     *OperationTrackerStore
		obj     xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{
						FrameworkProvider: &mockTPFProvider{},
					}, nil
				},
				cfg: newBaseUpjetConfig(),
				obj: func() xpresource.Managed { o := newBaseObject(); return &o }(),
				ots: ots,
			},
		},
		// Verifies that the context passed to SetupFn is independent of the
		// reconciliation context. Canceling the reconciliation context must not
		// affect the async context stored inside provider configuration by the
		// provider's SetupFn.
		"SetupFnReceivesContextIndependentOfReconciliationContext": {
			args: args{
				setupFn: func(ctx context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					// The context received here must not be derived from the
					// reconciliation context. We verify this by checking that
					// the context has a deadline (from defaultAsyncTimeout) and
					// is not already canceled.
					if err := ctx.Err(); err != nil {
						return terraform.Setup{}, err
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Error("setupFn: expected a context with a deadline (defaultAsyncTimeout), got none")
					}
					return terraform.Setup{
						FrameworkProvider: &mockTPFProvider{},
					}, nil
				},
				cfg: newBaseUpjetConfig(),
				obj: func() xpresource.Managed { o := newBaseObject(); return &o }(),
				ots: ots,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
			// Cancel the reconciliation context before Connect is called to
			// simulate a short-lived reconciliation deadline expiring.
			reconcileCancel()

			c := NewTerraformPluginFrameworkAsyncConnector(nil, tc.args.ots, tc.args.setupFn, tc.args.cfg, WithTerraformPluginFrameworkAsyncLogger(logTest))
			_, err := c.Connect(reconcileCtx, tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginFrameworkDisconnect(t *testing.T) {
	type args struct {
		operationRunning bool
	}
	type want struct {
		cancelCalled bool
		err          error
	}
	cases := map[string]struct {
		args
		want
	}{
		"CancelsAsyncContextWhenNoOperationRunning": {
			args: args{operationRunning: false},
			want: want{cancelCalled: true},
		},
		"DoesNotCancelAsyncContextWhenOperationRunning": {
			args: args{operationRunning: true},
			want: want{cancelCalled: false},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cancelCalled := false
			e := prepareTerraformPluginFrameworkAsyncExternalClient(newBaseUpjetConfig())
			e.asyncCancel = func() { cancelCalled = true }
			if tc.args.operationRunning {
				e.opTracker.LastOperation.MarkStart("create")
			}
			err := e.Disconnect(context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDisconnect(...): -want error, +got error:\n", diff)
			}
			if cancelCalled != tc.want.cancelCalled {
				t.Errorf("Disconnect(...): asyncCancel called = %v, want %v", cancelCalled, tc.want.cancelCalled)
			}
		})
	}
}

package payment

import (
	"context"
	"net/http"
	"strconv"

	"nofx/mcp"
	"nofx/store"
)

// ChargeContext carries metadata for recording a Claw402/x402 settlement.
type ChargeContext struct {
	TraderID string
	Source   string
	Model    string
	Provider string
}

type chargeCtxKey struct{}

// WithChargeContext attaches charge metadata to ctx.
func WithChargeContext(ctx context.Context, cc ChargeContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, chargeCtxKey{}, cc)
}

// ChargeContextFrom returns charge metadata stored in ctx.
func ChargeContextFrom(ctx context.Context) (ChargeContext, bool) {
	if ctx == nil {
		return ChargeContext{}, false
	}
	cc, ok := ctx.Value(chargeCtxKey{}).(ChargeContext)
	return cc, ok
}

// WithChargeSource returns ctx with the same trader/model/provider but a new source label.
func WithChargeSource(ctx context.Context, source string) context.Context {
	cc, ok := ChargeContextFrom(ctx)
	if !ok {
		return ctx
	}
	cc.Source = source
	return WithChargeContext(ctx, cc)
}

// ChargeRecorder persists a settled Claw402 charge. Registered from main.go.
type ChargeRecorder func(ctx context.Context, costUSD float64)

var chargeRecorder ChargeRecorder

// RegisterChargeRecorder wires the global charge persistence hook.
func RegisterChargeRecorder(fn ChargeRecorder) {
	chargeRecorder = fn
}

// RecordCharge invokes the registered recorder when ctx carries a trader ID.
func RecordCharge(ctx context.Context, costUSD float64) {
	if chargeRecorder == nil || costUSD <= 0 {
		return
	}
	cc, ok := ChargeContextFrom(ctx)
	if !ok || cc.TraderID == "" {
		return
	}
	chargeRecorder(ctx, costUSD)
}

func settledUSDFromHeader(h http.Header) float64 {
	if h == nil {
		return 0
	}
	if v := h.Get(X402SettledUSDHeader); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 0
}

// RecordChargeFromHeader records a non-stream settlement using the gateway header.
func RecordChargeFromHeader(ctx context.Context, h http.Header) {
	RecordCharge(ctx, settledUSDFromHeader(h))
}

// RecordChargeFromClient records a stream (or header) settlement using client state.
func RecordChargeFromClient(ctx context.Context, c *mcp.Client) {
	if chargeRecorder == nil || c == nil {
		return
	}
	cc, ok := ChargeContextFrom(ctx)
	if !ok || cc.TraderID == "" {
		return
	}

	cost := c.LastCallSettledUSD
	if cost <= 0 && c.LastCallUsage != nil {
		model := cc.Model
		if model == "" {
			model = c.Model
		}
		if computed, ok := store.ComputeUsageCost(model, c.LastCallUsage.PromptTokens, c.LastCallUsage.CompletionTokens); ok {
			cost = computed
		}
	}
	if cost <= 0 {
		model := cc.Model
		if model == "" {
			model = c.Model
		}
		cost = store.GetModelPrice(model)
	}
	if cost <= 0 {
		return
	}
	chargeRecorder(ctx, cost)
}

func chargeContextFromClient(c *mcp.Client) context.Context {
	if c != nil && c.ChargeCtx != nil {
		return c.ChargeCtx
	}
	return context.Background()
}

package band

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

// Hook is an interface of hook that can process along with abci application
type Hook interface {
	AfterInitChain(ctx sdk.Context, req abci.RequestInitChain, res abci.ResponseInitChain)
	AfterBeginBlock(ctx sdk.Context, req abci.RequestBeginBlock, res abci.ResponseBeginBlock)
	AfterDeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, res abci.ResponseDeliverTx)
	AfterEndBlock(ctx sdk.Context, req abci.RequestEndBlock, res abci.ResponseEndBlock)
	BeforeCommit()
	CustomQuery(path string) (result []byte, match bool, err error)
}

type Hooks []Hook

func (h Hooks) AfterInitChain(ctx sdk.Context, req abci.RequestInitChain, res abci.ResponseInitChain) {
	for _, hook := range h {
		hook.AfterInitChain(ctx, req, res)
	}
}

func (h Hooks) AfterBeginBlock(ctx sdk.Context, req abci.RequestBeginBlock, res abci.ResponseBeginBlock) {
	for _, hook := range h {
		hook.AfterBeginBlock(ctx, req, res)
	}
}

func (h Hooks) AfterDeliverTx(ctx sdk.Context, req abci.RequestDeliverTx, res abci.ResponseDeliverTx) {
	for _, hook := range h {
		hook.AfterDeliverTx(ctx, req, res)
	}
}

func (h Hooks) AfterEndBlock(ctx sdk.Context, req abci.RequestEndBlock, res abci.ResponseEndBlock) {
	for _, hook := range h {
		hook.AfterEndBlock(ctx, req, res)
	}
}

func (h Hooks) BeforeCommit() {
	for _, hook := range h {
		hook.BeforeCommit()
	}
}

func (h Hooks) CustomQuery(path string) (result []byte, match bool, err error) {
	for _, hook := range h {
		res, match, err := hook.CustomQuery(path)
		if match {
			return res, true, err
		}
	}
	return nil, false, nil
}

package server

// BaseResp ...
type BaseResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// PriceResult ...
type PriceResult struct {
	BaseResp
	Prices []float64 `json:"prices"`
}

// TokensResult ...
type TokensResult struct {
	BaseResp
	Tokens []string `json:"tokens"`
}

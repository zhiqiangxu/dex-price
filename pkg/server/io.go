package server

// BaseResp ...
type BaseResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// TokenPrice ...
type TokenPrice struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

// PriceResult ...
type PriceResult struct {
	BaseResp
	Prices []TokenPrice `json:"prices"`
}

// TokensResult ...
type TokensResult struct {
	BaseResp
	Tokens []string `json:"tokens"`
}

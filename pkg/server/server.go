package server

import (
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/zhiqiangxu/dex-price/config"
)

type tokenRoute struct {
	chain     *config.Chain
	swap      *config.Swap
	pairIndex int
}

type priceCache struct {
	price float64
	ts    int64
}

// Server ...
type Server struct {
	conf *config.Config
	g    *gin.Engine

	routes map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *tokenRoute

	mu          sync.RWMutex
	priceCaches map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *priceCache
}

func New(conf *config.Config) *Server {
	g := gin.New()
	g.Use(gin.Recovery())

	routes := make(map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *tokenRoute)
	for _, chain := range conf.Chains {
		chainRoute := make(map[string] /*swap*/ map[string] /*token*/ *tokenRoute)
		for _, swap := range chain.Swaps {
			swapRoute := make(map[string] /*token*/ *tokenRoute)
			for i, pair := range swap.Pairs {
				swapRoute[pair.TargetTokenName] = &tokenRoute{chain: chain, swap: swap, pairIndex: i}
			}
			chainRoute[swap.Name] = swapRoute
		}
		routes[chain.Name] = chainRoute
	}

	s := &Server{conf: conf, g: g, routes: routes, priceCaches: make(map[string]map[string]map[string]*priceCache)}
	s.registerHandlers(g)

	return s
}

func (s *Server) Start() (err error) {
	s.g.Run(fmt.Sprintf("0.0.0.0:%d", s.conf.Listen))
	return
}

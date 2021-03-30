package server

import (
	"fmt"
	"log"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/zhiqiangxu/dex-price/config"
)

type tokenConstant struct {
	pairAddr            common.Address
	targetTokenDecimals uint8
	priceTokenDecimals  uint8
	targetTokenIs0      bool
}

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
	ethClientIndex int64

	conf *config.Config
	g    *gin.Engine

	routes map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *tokenRoute

	mu          sync.RWMutex
	priceCaches map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *priceCache

	constantMu     sync.RWMutex
	tokenConstants map[string] /*chain*/ map[string] /*swap*/ map[string] /*token*/ *tokenConstant

	ethClients []*ethclient.Client
}

func New(conf *config.Config) *Server {
	g := gin.New()
	g.Use(gin.Recovery())

	var ethClients []*ethclient.Client
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

		if chain.Name == "eth" {
			for _, node := range chain.Nodes {
				client, err := ethclient.Dial(node)
				if err != nil {
					log.Fatal(fmt.Sprintf("ethclient.Dial failed:%v", err))
				}
				ethClients = append(ethClients, client)
			}
		} else {
			log.Fatal(fmt.Sprintf("chain %s not supported yet", chain.Name))
		}
	}

	s := &Server{
		conf:           conf,
		g:              g,
		routes:         routes,
		priceCaches:    make(map[string]map[string]map[string]*priceCache),
		tokenConstants: make(map[string]map[string]map[string]*tokenConstant),
		ethClients:     ethClients}
	s.registerHandlers(g)

	return s
}

func (s *Server) Start() (err error) {
	s.g.Run(fmt.Sprintf("0.0.0.0:%d", s.conf.Listen))
	return
}

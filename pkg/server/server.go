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

	routes map[string] /*token*/ *tokenRoute

	mu          sync.RWMutex
	priceCaches map[string] /*token*/ *priceCache

	constantMu     sync.RWMutex
	tokenConstants map[string] /*token*/ *tokenConstant

	ethClients []*ethclient.Client

	stableCoins map[string]bool
}

func New(conf *config.Config) *Server {
	g := gin.New()
	g.Use(gin.Recovery())

	var ethClients []*ethclient.Client
	routes := make(map[string]*tokenRoute)
	stableCoins := make(map[string]bool)
	for _, chain := range conf.Chains {
		for _, swap := range chain.Swaps {
			for i, pair := range swap.Pairs {
				if routes[pair.TargetTokenName] != nil {
					log.Fatal(fmt.Sprintf("duplicate token:%s", pair.TargetTokenName))
				}
				routes[pair.TargetTokenName] = &tokenRoute{chain: chain, swap: swap, pairIndex: i}
			}
		}

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

		for _, stableCoin := range chain.StableCoins {
			if stableCoins[stableCoin] {
				log.Fatal(fmt.Sprintf("duplicate stableCoin:%s", stableCoin))
			}
			stableCoins[stableCoin] = true
		}
	}

	s := &Server{
		conf:           conf,
		g:              g,
		routes:         routes,
		priceCaches:    make(map[string]*priceCache),
		tokenConstants: make(map[string]*tokenConstant),
		ethClients:     ethClients,
		stableCoins:    stableCoins}
	s.registerHandlers(g)

	return s
}

func (s *Server) Start() (err error) {
	s.g.Run(fmt.Sprintf("0.0.0.0:%d", s.conf.Listen))
	return
}

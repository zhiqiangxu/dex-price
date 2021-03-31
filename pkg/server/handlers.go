package server

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/zhiqiangxu/dex-price/pkg/abi/erc20"
	"github.com/zhiqiangxu/dex-price/pkg/abi/uni"
)

func (s *Server) registerHandlers(g *gin.Engine) {

	g.GET("/price/:tokens", s.queryPriceHandler)
	g.GET("/tokens", s.queryTokensHandler)
}

const cacheExpireSeconds = 1

func (s *Server) queryTokensHandler(c *gin.Context) {
	var tokens []string
	for token := range s.routes {
		tokens = append(tokens, token)
	}

	var output TokensResult
	output.Tokens = tokens
	output.Code = http.StatusOK
	c.JSON(http.StatusOK, output)
}

func (s *Server) queryPriceHandler(c *gin.Context) {

	result := make(map[string]float64)
	tokens := strings.Split(c.Param("tokens"), ",")
	tokensToQuery := make([]string, 0, len(tokens))

	now := time.Now().Unix()

	s.mu.RLock()

	for _, token := range tokens {
		cache := s.priceCaches[token]
		if cache != nil && cache.ts+cacheExpireSeconds >= now {
			result[token] = cache.price
		} else {
			tokensToQuery = append(tokensToQuery, token)
		}
	}

	s.mu.RUnlock()

	var queriedPrices []float64
	for _, token := range tokensToQuery {
		if s.routes[token] == nil {
			c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("token not found:%s", token)})
			return
		}

		tokenRoute := s.routes[token]
		price, err := s.queryPrice(tokenRoute)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("queryPrice fail:%v", err)})
			return
		}
		priceToken := tokenRoute.swap.Pairs[tokenRoute.pairIndex].PriceTokenName
		if !s.stableCoins[priceToken] {
			priceTokenRoute := s.routes[priceToken]
			if priceTokenRoute == nil {
				c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("priceToken not found:%s", priceToken)})
			}
			priceTokenPrice, err := s.queryPrice(priceTokenRoute)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("priceTokenPrice queryPrice fail:%v", err)})
				return
			}
			price = price * priceTokenPrice
		}
		queriedPrices = append(queriedPrices, price)
		result[token] = price
	}

	now = time.Now().Unix()
	s.mu.Lock()
	for i, token := range tokensToQuery {
		s.priceCaches[token] = &priceCache{price: queriedPrices[i], ts: now}
	}
	s.mu.Unlock()

	var output PriceResult
	for _, token := range tokens {
		output.Prices = append(output.Prices, TokenPrice{Symbol: token, Price: result[token]})
	}
	output.Code = http.StatusOK
	c.JSON(http.StatusOK, output)
}

func (s *Server) updateTokenConstant(route *tokenRoute, client *ethclient.Client) (constant *tokenConstant, err error) {
	factoryAddr := common.HexToAddress(route.swap.Factory)
	factoryCaller, err := uni.NewIUniswapV2FactoryCaller(factoryAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIUniswapV2FactoryCaller fail:%v", err)
		return
	}
	pair := route.swap.Pairs[route.pairIndex]
	targetTokenAddr := common.HexToAddress(pair.TargetTokenAddr)
	priceTokenAddr := common.HexToAddress(pair.PriceTokenAddr)
	pairAddr, err := factoryCaller.GetPair(nil, targetTokenAddr, priceTokenAddr)
	if err != nil {
		err = fmt.Errorf("GetPair fail:%v", err)
		return
	}

	pairContract, err := uni.NewIUniswapV2Pair(pairAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIUniswapV2Pair fail:%v", err)
		return
	}
	targetTokenContract, err := erc20.NewIERC20(targetTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("targetTokenContract erc20.NewIERC20 fail:%v", err)
		return
	}
	priceTokenContract, err := erc20.NewIERC20(priceTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("priceTokenContract erc20.NewIERC20 fail:%v", err)
		return
	}
	targetTokenDecimals, err := targetTokenContract.Decimals(nil)
	if err != nil {
		err = fmt.Errorf("targetTokenContract.Decimals fail:%v", err)
		return
	}
	priceTokenDecimals, err := priceTokenContract.Decimals(nil)
	if err != nil {
		err = fmt.Errorf("priceTokenContract.Decimals fail:%v", err)
		return
	}

	token0Addr, err := pairContract.Token0(nil)
	if err != nil {
		err = fmt.Errorf("Token0 fail:%v", err)
		return
	}

	token1Addr, err := pairContract.Token1(nil)
	if err != nil {
		err = fmt.Errorf("Token1 fail:%v", err)
		return
	}

	if !((targetTokenAddr == token0Addr && priceTokenAddr == token1Addr) || (targetTokenAddr == token1Addr && priceTokenAddr == token0Addr)) {
		err = fmt.Errorf("invalid pair for %s", pair.TargetTokenName)
		return
	}

	constant = &tokenConstant{pairAddr: pairAddr, targetTokenDecimals: targetTokenDecimals, priceTokenDecimals: priceTokenDecimals, targetTokenIs0: targetTokenAddr == token0Addr}

	s.constantMu.Lock()

	s.tokenConstants[pair.TargetTokenName] = constant
	s.constantMu.Unlock()
	return
}

func (s *Server) queryPrice(route *tokenRoute) (price float64, err error) {
	defer func() {
		if err != nil {
			fmt.Println("queryPrice error", err)
		}
	}()

	if route.chain.Name != "eth" {
		err = fmt.Errorf("chain %s not supported yet", route.chain.Name)
		return
	}

	index := atomic.AddInt64(&s.ethClientIndex, 1)
	client := s.ethClients[int(index)%len(s.ethClients)]

	pair := route.swap.Pairs[route.pairIndex]
	s.constantMu.RLock()
	constantCache := s.tokenConstants[pair.TargetTokenName]
	s.constantMu.RUnlock()
	if constantCache == nil {
		constantCache, err = s.updateTokenConstant(route, client)
		if err != nil {
			err = fmt.Errorf("updateTokenConstant fail:%v", err)
			return
		}
	}

	targetTokenAddr := common.HexToAddress(pair.TargetTokenAddr)
	priceTokenAddr := common.HexToAddress(pair.PriceTokenAddr)

	targetTokenContract, err := erc20.NewIERC20(targetTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIERC20 fail:%v", err)
		return
	}

	priceTokenContract, err := erc20.NewIERC20(priceTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIERC20 fail:%v", err)
		return
	}

	pairContract, err := uni.NewIUniswapV2Pair(constantCache.pairAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIUniswapV2Pair fail:%v", err)
		return
	}

	price, err = calcPrice(pairContract, targetTokenContract, priceTokenContract, constantCache.targetTokenDecimals, constantCache.priceTokenDecimals, constantCache.targetTokenIs0)
	if err != nil {
		err = fmt.Errorf("calcPrice fail:%v", err)
		return
	}
	return
}

func calcPrice(pairContract *uni.IUniswapV2Pair, targetTokenContract, priceTokenContract *erc20.IERC20, targetTokenDecimals, priceTokenDecimals uint8, targetTokenIs0 bool) (price float64, err error) {
	r, err := pairContract.GetReserves(nil)
	if err != nil {
		err = fmt.Errorf("GetReserves fail:%v", err)
		return
	}

	if targetTokenIs0 {

		denominator := big.NewFloat(0).Quo(big.NewFloat(0).SetInt(r.Reserve0), big.NewFloat(0).SetInt(big.NewInt(intPow(10, int64(targetTokenDecimals)))))
		numerator := big.NewFloat(0).Quo(big.NewFloat(0).SetInt(r.Reserve1), big.NewFloat(0).SetInt(big.NewInt(intPow(10, int64(priceTokenDecimals)))))
		price, _ = big.NewFloat(0).Quo(numerator, denominator).Float64()
		return

	} else {

		denominator := big.NewFloat(0).Quo(big.NewFloat(0).SetInt(r.Reserve1), big.NewFloat(0).SetInt(big.NewInt(intPow(10, int64(targetTokenDecimals)))))
		numerator := big.NewFloat(0).Quo(big.NewFloat(0).SetInt(r.Reserve0), big.NewFloat(0).SetInt(big.NewInt(intPow(10, int64(priceTokenDecimals)))))
		price, _ = big.NewFloat(0).Quo(numerator, denominator).Float64()
		return

	}
}

func intPow(n, m int64) int64 {
	if m == 0 {
		return 1
	}
	result := n
	for i := int64(2); i <= m; i++ {
		result *= n
	}
	return result
}

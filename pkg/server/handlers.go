package server

import (
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/zhiqiangxu/dex-price/pkg/abi/erc20"
	"github.com/zhiqiangxu/dex-price/pkg/abi/uni"
)

func (s *Server) registerHandlers(g *gin.Engine) {

	g.GET("/price/:chainName/:swapName/:tokens", s.queryPrice)
}

const cacheExpireSeconds = 1

func (s *Server) queryPrice(c *gin.Context) {

	chainName := c.Param("chainName")
	swapName := c.Param("swapName")

	swapMap := s.routes[chainName][swapName]
	if len(swapMap) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"msg": "chain->swap not found"})
		return
	}

	result := make(map[string]float64)
	tokens := strings.Split(c.Param("tokens"), ",")
	tokensToQuery := make([]string, 0, len(tokens))

	now := time.Now().Unix()

	s.mu.RLock()

	for _, token := range tokens {
		cache := s.priceCaches[chainName][swapName][token]
		if cache != nil && cache.ts+cacheExpireSeconds >= now {
			result[token] = cache.price
		} else {
			tokensToQuery = append(tokensToQuery, token)
		}
	}

	s.mu.RUnlock()

	var queriedPrices []float64
	for _, token := range tokensToQuery {
		if swapMap[token] == nil {
			c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("token not found:%s", token)})
			return
		}

		price, err := queryPrice(swapMap[token])
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"msg": fmt.Sprintf("queryPrice fail:%v", err)})
			return
		}
		queriedPrices = append(queriedPrices, price)
		result[token] = price
	}

	now = time.Now().Unix()
	s.mu.Lock()
	if s.priceCaches[chainName] == nil {
		s.priceCaches[chainName] = make(map[string]map[string]*priceCache)
	}
	if s.priceCaches[chainName][swapName] == nil {
		s.priceCaches[chainName][swapName] = make(map[string]*priceCache)
	}
	for i, token := range tokensToQuery {
		s.priceCaches[chainName][swapName][token] = &priceCache{price: queriedPrices[i], ts: now}
	}
	s.mu.Unlock()

	var output PriceResult
	for _, token := range tokens {
		output.Prices = append(output.Prices, result[token])
	}
	output.Code = http.StatusOK
	c.JSON(http.StatusOK, output)
}

// TODO cache client
func queryPrice(route *tokenRoute) (price float64, err error) {
	defer func() {
		if err != nil {
			fmt.Println("queryPrice error", err)
		}
	}()

	client, err := ethclient.Dial(route.chain.Nodes[0])
	if err != nil {
		err = fmt.Errorf("ethclient.Dial fail:%v", err)
		return
	}
	defer client.Close()

	factoryAddr := common.HexToAddress(route.swap.Factory)

	targetTokenAddr := common.HexToAddress(route.swap.Pairs[route.pairIndex].TargetTokenAddr)
	priceTokenAddr := common.HexToAddress(route.swap.Pairs[route.pairIndex].PriceTokenAddr)

	targetTokenContract, err := erc20.NewIERC20(targetTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIERC20 fail:%v", err)
		return
	}
	targetTokenDecimals, err := targetTokenContract.Decimals(nil)
	if err != nil {
		err = fmt.Errorf("targetTokenContract.Decimals fail:%v", err)
		return
	}
	priceTokenContract, err := erc20.NewIERC20(priceTokenAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIERC20 fail:%v", err)
		return
	}
	priceTokenDecimals, err := priceTokenContract.Decimals(nil)
	if err != nil {
		err = fmt.Errorf("priceTokenContract.Decimals fail:%v", err)
		return
	}

	factoryCaller, err := uni.NewIUniswapV2FactoryCaller(factoryAddr, client)
	if err != nil {
		err = fmt.Errorf("NewIUniswapV2FactoryCaller fail:%v", err)
		return
	}

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
		err = fmt.Errorf("invalid pair")
		return
	}

	price, err = calcPrice(pairContract, targetTokenContract, priceTokenContract, targetTokenDecimals, priceTokenDecimals, targetTokenAddr == token0Addr)
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

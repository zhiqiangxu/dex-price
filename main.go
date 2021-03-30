package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/zhiqiangxu/dex-price/config"
	"github.com/zhiqiangxu/dex-price/pkg/server"
)

var confFile string

func init() {
	flag.StringVar(&confFile, "conf", "./config.json", "configuration file path")
}

func main() {

	conf, err := config.LoadConfig(confFile)
	if err != nil {
		log.Fatal("LoadConfig fail", err)
	}

	{
		confBytes, _ := json.MarshalIndent(conf, "", "    ")
		fmt.Println("conf", string(confBytes))
	}

	s := server.New(conf)

	err = s.Start()

	fmt.Println("server quit", err)

}

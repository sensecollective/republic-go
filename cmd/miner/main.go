package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"time"

	"github.com/republicprotocol/go-miner"
)

var config *miner.Config

func main() {
	// Parse command line arguments and fill the miner.Config.
	if err := parseCommandLineFlags(); err != nil {
		log.Println(err)
		flag.Usage()
		return
	}

	// Create a new miner.Miner.
	miner, err := miner.NewMiner(config)
	if err != nil {
		log.Fatal(err)
	}

	// Start both gRPC servers.
	miner.Swarm.Register()
	miner.Xing.Register()
	go func() {
		listener, err := net.Listen("tcp", config.Host+":"+config.Port)
		if err != nil {
			log.Fatal(err)
		}
		if err := miner.Server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()
	time.Sleep(time.Second)

	// Establish connections to bootstrap swarm.Nodes.
	go func() {
		log.Println("establishing connections...")
		miner.EstablishConnections()
	}()
	time.Sleep(time.Second)

	// Begin computing compute.OrderFragments.
	log.Println("computing...")
	quit := make(chan struct{})
	miner.Mine(quit)
}

func parseCommandLineFlags() error {
	confFilename := flag.String("config", "", "Path to the JSON configuration file")

	flag.Parse()

	if *confFilename == "" {
		return errors.New("no config file given")
	}

	conf, err := miner.LoadConfig(*confFilename)
	if err != nil {
		return err
	}
	config = conf

	return nil
}

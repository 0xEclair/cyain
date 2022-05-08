package main

import (
	"fmt"
	"log"
	"net"
)

type version struct {
	Version    int
	BaseHeight int
	AddrFrom   string
}

const(
	protocol = "tcp"
)
var nodeAddress string
var knownNodes = []string{"localhost:3000"}

func StartServer(nodeID, minerAddress string) {
	nodeAddress = fmt.Sprintf("localhost:%s"nodeID)
	minerAddress = minerAddress
	ln, err := net.Listen(protocol, nodeAddress)
	if err != nil {
		log.Panic(err)
	}
	defer ln.Close()
	
	bc := NewBlockchain(nodeID)
	if nodeAddress != knownNodes[0] {
		sendVersion(knownNodes[0], bc)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Panic(err)
		}
		go handleConnection(conn, bc)
	}
}

func sendVersion(addr string, bc *BlockChain) {
	bestHeight := bc.GetBestHeight()
	payload := gobEncode(version{
		nodeVersion,
		bestHeight,
		nodeAddress,
	})
	
	request := append(commandToBytes("version"), payload...)
	
	sendData(addr, request)
}

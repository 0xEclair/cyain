package main

import (
	"fmt"
	"io/ioutil"
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
	nodeVersion = 1
	commandLength = 12
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

func commandToBytes(command string) []byte {
	var bytes [commandLength]byte
	for i, c := range command {
		bytes[i] = byte(c)
	}
	return bytes[:]
}

func bytesToCommand(bytes []byte) string {
	var command []byte
	
	for _, b := range bytes {
		if b != 0x0 {
			command = append(command, b)
		}
	}
	
	return fmt.Sprintf("%s", command)
}

func handleConnection(conn net.Conn, bc *BlockChain) {
	request, err := ioutil.ReadAll(conn)
	if err != nil {
		log.Panic(err)
	}
	command := bytesToCommand(request[:commandLength])
	fmt.Printf("received %s command\n", command)
	
	switch command {
	case "addr":
		handleAddr(request)
	case "block":
		handleBlock(request, bc)
	case "inv":
		handleInv(request, bc)
	case "getblocks":
		handleGetBlocks(request, bc)
	case "getdata":
		handleGetData(request, bc)
	case "tx":
		handleTx(request, bc)
	case "version":
		handleVersion(request, bc)
	default:
		fmt.Println("unknow command!")
	}
	
	conn.Close()
}
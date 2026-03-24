package main

import (
	"log"
	"os"

	miner "colossusx"
)

func main() {
	if err := miner.Main(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

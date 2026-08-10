// Command fake_cli is a harmless integration-test fixture for the managed
// Ookla upload path. It is not the proprietary Ookla CLI and is never shipped
// in the runtime image.
package main

import (
	"fmt"
	"os"
	"slices"
)

func main() {
	arguments := os.Args[1:]
	switch {
	case slices.Contains(arguments, "--version"):
		fmt.Println("Speedtest by Ookla 1.2.0.84 test fixture")
	case slices.Contains(arguments, "--servers"):
		fmt.Println(`{"servers":[{"id":"42","host":"speed.example.test","name":"Test server","sponsor":"MultiSpeed fixture","location":"Test","country":"ZZ","distance":0}]}`)
	default:
		fmt.Println(`{"type":"result","ping":{"jitter":1,"latency":10},"download":{"bandwidth":12500000,"bytes":1250000},"upload":{"bandwidth":6250000,"bytes":625000},"packetLoss":0,"interface":{"externalIp":"192.0.2.1"},"server":{"id":42,"host":"speed.example.test","name":"Test server","location":"Test","country":"ZZ","ip":"192.0.2.2","sponsor":"MultiSpeed fixture"},"result":{"url":"https://results.example.test/42"}}`)
	}
}

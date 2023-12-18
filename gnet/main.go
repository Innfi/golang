package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/panjf2000/gnet/v2"
)

type echoServer struct {
	gnet.BuiltinEventEngine

	eng       gnet.Engine
	addr      string
	multicore bool
}

func (es *echoServer) onBoot(eng gnet.Engine) gnet.Action {
	es.eng = eng
	log.Printf("echo server %t %s\n", es.multicore, es.addr)

	return gnet.None
}

func (es *echoServer) onTraffic(c gnet.Conn) gnet.Action {
	buf, _ := c.Next(-1)
	c.Write(buf)

	return gnet.None
}

func main() {
	fmt.Printf("here")

	var port int
	var multicore bool

	flag.IntVar(&port, "port", 9000, "--port 9000")
	flag.BoolVar(&multicore, "multicore", false, "--multicore true")
	flag.Parse()

	echo := &echoServer{
		addr:      fmt.Sprintf("tcp://:%d", port),
		multicore: multicore,
	}

	log.Fatal(
		gnet.Run(echo, echo.addr, gnet.WithMulticore(multicore)),
	)

}

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"github.com/nknorg/nkn-sdk-go/examples/session/grpc/proto"
	"google.golang.org/grpc"
	"log"
	"net"
	"strings"
	"time"

	nkn "github.com/nknorg/nkn-sdk-go"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

type xService struct {
	proto.UnimplementedXServer
}

func (x *xService) Hello(ctx context.Context, r *proto.Req) (*proto.Resp, error) {
	name := r.Msg
	return &proto.Resp{
		Msg: "hello " + name,
	}, nil
}

func main() {
	numClients := flag.Int("n", 1, "number of clients")
	seedHex := flag.String("s", "", "secret seed")
	dialAddr := flag.String("a", "", "dial address")
	dial := flag.Bool("d", false, "dial")
	listen := flag.Bool("l", false, "listen")

	flag.Parse()

	seed, err := hex.DecodeString(*seedHex)
	if err != nil {
		log.Fatal(err)
	}

	account, err := nkn.NewAccount(seed)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Seed:", hex.EncodeToString(account.Seed()))

	clientConfig := &nkn.ClientConfig{ConnectRetries: 1}

	if *listen {
		m, err := nkn.NewMultiClient(account, listenID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C
		log.Println("connected")
		log.Println("address: ", listenID+"."+strings.SplitN(m.Addr().String(), ".", 2)[1])
		err = m.Listen(nil)
		if err != nil {
			log.Fatal(err)
		}

		s := grpc.NewServer()
		proto.RegisterXServer(s, &xService{})
		err = s.Serve(m)
		if err != nil {
			log.Println(err.Error())
		}
	}

	if *dial {
		m, err := nkn.NewMultiClient(account, dialID, *numClients, false, clientConfig)
		if err != nil {
			log.Fatal(err)
		}

		<-m.OnConnect.C
		log.Println("connected")

		if len(*dialAddr) == 0 {
			*dialAddr = listenID + "." + strings.SplitN(m.Addr().String(), ".", 2)[1]
		}

		var opts []grpc.DialOption
		opts = append(opts, grpc.WithInsecure())
		opts = append(opts, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return m.Dial(addr)
		}))
		conn, err := grpc.Dial(*dialAddr, opts...)
		client := proto.NewXClient(conn)
		resp, err := client.Hello(context.Background(), &proto.Req{Msg: dialID})
		if err == nil {
			log.Println("response from grpc server: ", resp.Msg)
		}
	}
}

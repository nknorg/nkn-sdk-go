# nkn-sdk-go

[![GoDoc](https://godoc.org/github.com/nknorg/nkn-sdk-go?status.svg)](https://godoc.org/github.com/nknorg/nkn-sdk-go) [![GitHub license](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/nknorg/nkn-sdk-go)](https://goreportcard.com/report/github.com/nknorg/nkn-sdk-go) [![Build Status](https://travis-ci.org/nknorg/nkn-sdk-go.svg?branch=master)](https://travis-ci.org/nknorg/nkn-sdk-go) [![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](#contributing)

Go implementation of NKN client and wallet SDK. The SDK consists of a
few components:

- [NKN Client](#client): Send and receive data for free between any NKN clients
  regardless their network condition without setting up a server or relying on
  any third party services. Data are end to end encrypted by default. Typically
  you might want to use [multiclient](#multiclient) instead of using client
  directly.

- [NKN MultiClient](#multiclient): Send and receive data using multiple NKN
  clients concurrently to improve reliability and latency. In addition, it
  supports session mode, a reliable streaming protocol similar to TCP based on
  [ncp](https://github.com/nknorg/ncp-go).

- [NKN Wallet](#wallet): Wallet SDK for [NKN
  blockchain](https://github.com/nknorg/nkn). It can be used to create wallet,
  transfer token to NKN wallet address, register name, subscribe to topic, etc.

Advantages of using NKN client/multiclient for data transmission:

- Network agnostic: Neither sender nor receiver needs to have public IP address
  or port forwarding. NKN clients only establish outbound (websocket)
  connections, so Internet access is all they need. This is ideal for client
  side peer to peer communication.

- Top level security: All data are end to end authenticated and encrypted. No
  one else in the world except sender and receiver can see or modify the content
  of the data. The same public key is used for both routing and encryption,
  eliminating the possibility of man in the middle attack.

- Decent performance: By aggregating multiple overlay paths concurrently,
  multiclient can get ~100ms end to end latency and 10+mbps end to end session
  throughput between international devices.

- Everything is free, open source and decentralized. (If you are curious, node
  relay traffic for clients for free to earn mining rewards in NKN blockchain.)

Documentation:
[https://godoc.org/github.com/nknorg/nkn-sdk-go](https://godoc.org/github.com/nknorg/nkn-sdk-go).


## Client

NKN Client provides low level p2p messaging through NKN network. For most
applications, it's more suitable to use multiclient (see
[multiclient](#multiclient) section below) for better reliability, lower
latency, and session mode support.

Create a client with a generated key pair:

```go
account, err := NewAccount(nil)
client, err := NewClient(account, "", nil)
```

Or with an identifier (used to distinguish different clients sharing the same
key pair):

```go
client, err := NewClient(account, "any string", nil)
```

Get client key pair:

```go
fmt.Println(account.Seed(), account.PubKey())
```

Create a client using an existing secret seed:

```go
seed, err := hex.DecodeStrings("039e481266e5a05168c1d834a94db512dbc235877f150c5a3cc1e3903672c673")
account, err := NewAccount(seed)
client, err := NewClient(account, "any string", nil)
```

Secret seed should be kept **SECRET**! Never put it in version control system
like here.

By default the client will use bootstrap RPC server (for getting node address)
provided by NKN. Any NKN full node can serve as a bootstrap RPC server. To
create a client using customized bootstrap RPC server:

```go
conf := &ClientConfig{SeedRPCServerAddr: NewStringArray("https://ip:port", "https://ip:port", ...)}
client, err := NewClient(account, "any string", conf)
```

Get client NKN address, which is used to receive data from other clients:

```go
fmt.Println(client.Address())
```

Listen for connection established:

```go
<- client.OnConnect.C
fmt.Println("Connection opened.")
```

Send text message to other clients:

```go
response, err := client.Send(NewStringArray("another client address"), []byte("hello world!"), nil)
```

You can also send byte array directly:

```go
response, err := client.Send(NewStringArray("another client address"), []byte{1, 2, 3, 4, 5}, nil)
```

Or publish a message to a specified topic (see wallet section for subscribing to
topics):

```go
client.Publish("topic", []byte("hello world!"), nil)
```

Receive data from other clients:

```go
msg := <- client.OnMessage.C
fmt.Println("Receive message from", msg.Src + ":", string(msg.Payload))
msg.Reply([]byte("response"))
```

Get 100 subscribers of specified topic starting from 0 offset, including those
in tx pool (fetch meta):

```go
subscribers, err := client.GetSubscribers("topic", 0, 100, true, true)
fmt.Println(subscribers.Subscribers, subscribers.SubscribersInTxPool)
```

Get subscribers count for specified topic:

```go
count, err := client.GetSubscribersCount("topic")
```

Get subscription:

```go
subscription, err := client.GetSubscription("topic", "identifier.publickey")
fmt.Printf("%+v\n", subscription) // &{Meta:meta ExpiresAt:100000}
```

## Multiclient

Multiclient creates multiple client instances by adding identifier prefix
(`__0__.`, `__1__.`, `__2__.`, ...) to a nkn address and send/receive packets
concurrently. This will greatly increase reliability and reduce latency at the
cost of more bandwidth usage (proportional to the number of clients).

Multiclient basically has the same API as client, except for a few more
initial configurations:

```go
numSubClients := 3
originalClient := false
multiclient, err := NewMultiClient(account, identifier, numSubClient, originalClient)
```

where `originalClient` controls whether a client with original identifier
(without adding any additional identifier prefix) will be created, and
`numSubClients` controls how many sub-clients to create by adding prefix
`__0__.`, `__1__.`, `__2__.`, etc. Using `originalClient == true` and
`numSubClients == 0` is equivalent to using a standard client without any
modification to the identifier. Note that if you use `originalClient == true`
and `numSubClients` is greater than 0, your identifier should not starts with
`__X__` where `X` is any number, otherwise you may end up with identifier
collision.

Any additional options will be passed to NKN client.

multiclient instance shares the same API as regular NKN client, see above for
usage and examples. If you need low-level property or API, you can use
`multiclient.DefaultClient` to get the default client and `multiclient.Clients`
to get all clients.

## Session

Multiclient supports a reliable transmit protocol called session. It will be
responsible for retransmission and ordering just like TCP. It uses multiple
clients to send and receive data in multiple path to achieve better throughput.
Unlike regular multiclient message, no redundant data is sent unless packet
loss.

Any multiclient can start listening for incoming session where the remote
address match any of the given regexp:

```go
multiclient, err := NewMultiClient(...)
// Accepting any address, equivalent to multiclient.Listen(NewStringArray(".*"))
err = multiclient.Listen(nil)
// Only accepting pubkey 25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99 but with any identifiers
err = multiclient.Listen(NewStringArray("25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99$"))
// Only accepting address alice.25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99
err = multiclient.Listen(NewStringArray("^alice\\.25d660916021ab1d182fb6b52d666b47a0f181ed68cf52a056041bdcf4faaf99$"))
```

Then it can start accepting sessions:

```go
session, err := multiclient.Accept()
```

Multiclient implements `net.Listener` interface, so one can use it as a drop-in
replacement when `net.Listener` is needed, e.g. `http.Serve`.

On the other hand, any multiclient can dial a session to a remote NKN address:

```go
session, err := multiclient.Dial("another nkn address")
```

Session implements `net.Conn` interface, so it can be used as a drop-in
replacement when `net.Conn` is needed:

```go
buf := make([]byte, 1024)
n, err := session.Read(buf)
n, err := session.Write(buf)
```

## Wallet

Create wallet SDK:

```go
account, err := NewAccount(nil)
wallet, err := NewWallet(account, &nkn.WalletConfig{Password: "password"})
```

By default the wallet will use RPC server provided by `nkn.org`. Any NKN full
node can serve as a RPC server. To create a wallet using customized RPC server:

```go
conf := &WalletConfig{
  Password: "password",
  SeedRPCServerAddr: NewStringArray("https://ip:port", "https://ip:port", ...),
}
wallet, err := NewWallet(account, conf)
```

Export wallet to JSON string, where sensitive contents are encrypted by password
provided in config:

```go
walletJSON, err := wallet.ToJSON()
```

Load wallet from JSON string, note that the password needs to be the same as the
one provided when creating wallet:

```go
walletFromJSON, err := nkn.WalletFromJSON(walletJSON, &nkn.WalletConfig{Password: "password"})
```

Verify whether an address is a valid NKN wallet address:

```go
err := nkn.VerifyWalletAddress(wallet.Address())
```

Verify password of the wallet:

```go
ok := wallet.VerifyPassword("password")
```

Query asset balance for this wallet:

```go
balance, err := wallet.Balance()
if err == nil {
    log.Println("asset balance:", balance.String())
} else {
    log.Println("query balance fail:", err)
}
```

Query asset balance for address:

```go
balance, err := wallet.BalanceByAddress("NKNxxxxx")
```

Transfer asset to some address:

```go
txnHash, err := wallet.Transfer(account.WalletAddress(), "100", "0")
```

Open nano pay channel to specified address:

```go
// you can pass channel duration (in unit of blocks) after address and txn fee
// after expired new channel (with new id) will be created under-the-hood
// this means that receiver need to claim old channel and reset amount calculation
np, err := wallet.NewNanoPay(address, "0", 4320)
```

Increment channel balance by 100 NKN:

```go
txn, err := np.IncrementAmount("100")
```

Then you can pass the transaction to receiver, who can send transaction to
on-chain later:

```go
txnHash, err := wallet.SendRawTransaction(txn)
```

Register name for this wallet:

```go
txnHash, err = wallet.RegisterName("somename")
```

Delete name for this wallet:

```go
txnHash, err = wallet.DeleteName("somename")
```

Subscribe to specified topic for this wallet for next 100 blocks:

```go
txnHash, err = wallet.Subscribe("identifier", "topic", 100, "meta", "0")
```

Unsubscribe from specified topic:

```go
txnHash, err = wallet.Unsubscribe("identifier", "topic", "0")
```

## iOS/Android

This library is designed to work with
[gomobile](https://godoc.org/golang.org/x/mobile/cmd/gomobile) and run natively
on iOS/Android without any modification. You can use `gomobile bind` to compile
it to Objective-C framework for iOS:

```shell
gomobile bind -target=ios -ldflags "-s -w" github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/nkn/transaction
```

and Java AAR for Android:

```shell
gomobile bind -target=android -ldflags "-s -w" github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/nkn/transaction
```

It's recommended to use the latest version of gomobile that supports go modules.

## Contributing

**Can I submit a bug, suggestion or feature request?**

Yes. Please open an issue for that.

**Can I contribute patches?**

Yes, we appreciate your help! To make contributions, please fork the repo, push
your changes to the forked repo with signed-off commits, and open a pull request
here.

Please sign off your commit. This means adding a line "Signed-off-by: Name
<email>" at the end of each commit, indicating that you wrote the code and have
the right to pass it on as an open source patch. This can be done automatically
by adding -s when committing:

```shell
git commit -s
```

## Community

- [Forum](https://forum.nkn.org/)
- [Discord](https://discord.gg/c7mTynX)
- [Telegram](https://t.me/nknorg)
- [Reddit](https://www.reddit.com/r/nknblockchain/)
- [Twitter](https://twitter.com/NKN_ORG)

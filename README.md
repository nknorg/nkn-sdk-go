# nkn-sdk-go

Go implementation of NKN SDK.

**Note: This repository is in the early development stage and may not have all
functions working properly. It should be used only for testing now.**

## Usage

Before you use SDK please call:
```go
Init()
```

Resolve name to wallet address:
```go
address, _ := GetAddressByName("somename")
```

Get subscribers of bucket 0 of specified topic:
```go
subscribers, _ := GetSubscribers("topic", 0)
```

Get first available bucket of specified topic:
```go
bucket, _ := GetFirstAvailableTopicBucket("topic")
```

Get buckets count of specified topic:
```go
bucket, _ := GetTopicBucketsCount("topic")
```

## Client Usage

Create a client with a generated key pair:

```go
account, _ := vault.NewAccount()
client, _ := NewClient(account, "")
```

Or with an identifier (used to distinguish different clients sharing the same
key pair):

```go
client, _ := NewClient(account, "any string")
```

Get client key pair:

```go
fmt.PrintLn(account.PrivateKey, account.PublicKey)
```

Create a client using an existing private key:

```go
privateKey, _ := common.HexStringToBytes("cd5fa29ed5b0e951f3d1bce5997458706186320f1dd89156a73d54ed752a7f37")
account, _ := vault.NewAccountWithPrivatekey(privateKey)
client, _ := NewClient(account, "any string")
```

By default the client will use bootstrap RPC server (for getting node address)
provided by us. Any NKN full node can serve as a bootstrap RPC server. To create
a client using customized bootstrap RPC server:

```go
SeedRPCServerAddr = "https://ip:port"
client, _ := NewClient(account, "any string")
```

Private key should be kept **SECRET**! Never put it in version control system
like here.

Get client NKN address, which is used to receive data from other clients:

```go
fmt.Println(client.Address)
```

Listen for connection established:

```go
<- client.OnConnect
fmt.Println("Connection opened.")
```

Send text message to other clients:

```go
client.Send([]string{"another client address"}, []byte("hello world!"), 0)
```

You can also send byte array directly:

```go
client.Send([]string{"another client address"}, []byte{1, 2, 3, 4, 5}, 0)
```

Or publish text message to a bucket 0 of specified topic:

```go
client.Publish("topic", 0, []byte("hello world!"), 0)
```

Receive data from other clients:

```go
msg := <- client.OnMessage
fmt.Println("Receive binary message from", msg.Src + ":", string(msg.Payload))
```

Listen for new blocks mined:
```go
block := <- client.OnBlock
fmt.Println("New block mined:", block.Header.Height)
```

## Wallet Usage

Create wallet SDK:
```go
account, _ := vault.NewAccount()
w := NewWalletSDK(account)
```

Query asset balance for this wallet:
```go
balance, err := w.Balance()
if err == nil {
    log.Println("asset balance for this wallet is:", balance.String())
} else {
    log.Println("query balance fail:", err)
}
```

Transfer asset to some address:
```go
address, _ := account.ProgramHash.ToAddress()
txid, err := w.Transfer(address, "100")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Register name for this wallet (only a-z and length 8-12):
```go
txid, err = w.RegisterName("somename")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Delete name for this wallet:
```go
txid, err = w.DeleteName()
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Subscribe to bucket 0 of specified topic for this wallet for next 10 blocks:
```go
txid, err = w.Subscribe("identifier", "topic", 0, 10)
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Subscribe to first available bucket of specified topic for this wallet for next 10 blocks:
```go
txid, err = w.SubscribeToFirstAvailableBucket("identifier", "topic", 10)
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

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

* [Discord](https://discord.gg/c7mTynX)
* [Telegram](https://t.me/nknorg)
* [Reddit](https://www.reddit.com/r/nknblockchain/)
* [Twitter](https://twitter.com/NKN_ORG)

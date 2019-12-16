# nkn-sdk-go

Go implementation of NKN SDK.

**Note: This repository is in the early development stage and may not have all
functions working properly. It should be used only for testing now.**

## Usage

Before you use SDK please call:
```go
Init()
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
fmt.Println(account.PrivateKey, account.PublicKey)
```

Create a client using an existing seed:

```go
seed, _ := common.HexStringToBytes("039e481266e5a05168c1d834a94db512dbc235877f150c5a3cc1e3903672c673")
privateKey := crypto.GetPrivateKeyFromSeed(seed)
account, _ := vault.NewAccountWithPrivatekey(privateKey)
client, _ := NewClient(account, "any string")
```

By default the client will use bootstrap RPC server (for getting node address)
provided by us. Any NKN full node can serve as a bootstrap RPC server. To create
a client using customized bootstrap RPC server:

```go
client, _ := NewClient(account, "any string", ClientConfig{SeedRPCServerAddr: "https://ip:port"})
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
response, err := client.Send([]string{"another client address"}, []byte("hello world!"))
```

You can also send byte array directly:

```go
response, err := client.Send([]string{"another client address"}, []byte{1, 2, 3, 4, 5})
```

Or publish text message to a bucket 0 of specified topic:

```go
client.Publish("topic", 0, []byte("hello world!"))
```

Receive data from other clients:

```go
msg := <- client.OnMessage
fmt.Println("Receive binary message from", msg.Src + ":", string(msg.Payload))
msg.Reply([]byte("response"))
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

By default the wallet will use RPC server provided by us.
Any NKN full node can serve as a RPC server. To create
a wallet using customized RPC server:
```go
account, _ := vault.NewAccount()
w := NewWalletSDK(account, WalletConfig{SeedRPCServerAddr: "https://ip:port"})
```

Query asset balance for this wallet:
```go
balance, err := w.Balance()
if err == nil {
    log.Println("asset balance:", balance.String())
} else {
    log.Println("query balance fail:", err)
}
```

Query asset balance for address:
```go
balance, err := w.BalanceByAddress("address")
if err == nil {
    log.Println("asset balance:", balance.String())
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

Open nano pay channel to specified address:
```go
// you can optionally pass channel duration after address (default is 4320 blocks, roughly 24h)
// after expired new channel (with new id) will be created under-the-hood
// this means that receiver need to claim old channel and reset amount calculation
np, err := w.NewNanoPay(address)
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Increment channel balance by 100 NKN:
```go
txid, err = np.IncrementAmount("100")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```
		
Register name for this wallet:
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
txid, err = w.DeleteName("somename")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Resolve name to wallet address:
```go
address, _ := w.GetAddressByName("somename")
```

Subscribe to specified topic for this wallet for next 10 blocks:
```go
txid, err = w.Subscribe("identifier", "topic", 10, "meta")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Unsubscribe from specified topic:
```go
txid, err = w.Unsubscribe("identifier", "topic")
if err == nil {
    log.Println("success:", txid)
} else {
    log.Println("fail:", err)
}
```

Get subscription:
```go
subscription, _ := w.GetSubscription("topic", "identifier.publickey")
fmt.Printf("%+v\n", subscription) // &{Meta:meta ExpiresAt:100000}
```

Get 10 subscribers of specified topic starting from 0 offset, including those in tx pool (fetch meta):
```go
subscribers, subscribersInTxPool, _ := w.GetSubscribers("topic", 0, 10, true, true)
```

Get subscribers count for specified topic:
```go
count, _ := w.GetSubscribersCount("topic")
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

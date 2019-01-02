# nkn-client-go

Go implementation of NKN client.

Send and receive data between any NKN clients without setting up a server.

Note: This is a **client** version of the NKN protocol, which can send and
receive data but **not** relay data (mining). For **node** implementation which
can mine NKN token by relaying data, please refer to
[nkn](https://github.com/nknorg/nkn/).

**Note: This repository is in the early development stage and may not have all
functions working properly. It should be used only for testing now.**

## Usage

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

Or publish text message to a topic (subscribe is done through [nkn-wallet-js](https://github.com/nknorg/nkn-wallet-js)):

```go
client.Publish("topic", []byte("hello world!"), 0)
```

Receive data from other clients:

```go
msg := <- client.OnMessage
fmt.Println("Receive binary message from", msg.Src + ":", string(msg.Payload))
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

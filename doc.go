/*
Package nkn provides Go implementation of NKN client and wallet SDK. The SDK
consists of a few components:

1. NKN Client: Send and receive data for free between any NKN clients regardless
their network condition without setting up a server or relying on any third
party services. Data are end to end encrypted by default. Typically you might
want to use multiclient instead of using client directly.

2. NKN MultiClient: Send and receive data using multiple NKN clients
concurrently to improve reliability and latency. In addition, it supports
session mode, a reliable streaming protocol similar to TCP based on ncp
(https://github.com/nknorg/ncp-go).

3. NKN Wallet: Wallet SDK for NKN blockchain (https://github.com/nknorg/nkn). It
can be used to create wallet, transfer token to NKN wallet address, register
name, subscribe to topic, etc.

Feature Highlights

Advantages of using NKN client/multiclient for data transmission:

1. Network agnostic: Neither sender nor receiver needs to have public IP address
or port forwarding. NKN clients only establish outbound (websocket) connections,
so Internet access is all they need. This is ideal for client side peer to peer
communication.

2. Top level security: All data are end to end authenticated and encrypted. No
one else in the world except sender and receiver can see or modify the content
of the data. The same public key is used for both routing and encryption,
eliminating the possibility of man in the middle attack.

3. Decent performance: By aggregating multiple overlay paths concurrently,
multiclient can get ~100ms end to end latency and 10+mbps end to end session
throughput between international devices.

4. Everything is free, open source and decentralized. (If you are curious, node
relay traffic for clients for free to earn mining rewards in NKN blockchain.)

Gomobile

This library is designed to work with gomobile
(https://godoc.org/golang.org/x/mobile/cmd/gomobile) and run natively on
iOS/Android without any modification. You can use gomobile to compile it to
Objective-C framework for iOS:

  gomobile bind -target=ios -ldflags "-s -w" github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/nkn/v2/transaction github.com/nknorg/nkngomobile

and Java AAR for Android:

  gomobile bind -target=android -ldflags "-s -w" github.com/nknorg/nkn-sdk-go github.com/nknorg/ncp-go github.com/nknorg/nkn/v2/transaction github.com/nknorg/nkngomobile

It's recommended to use the latest version of gomobile that supports go modules.
*/
package nkn

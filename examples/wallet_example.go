package main

import (
	"fmt"
	"log"

	. "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/vault"
)

func main() {
	err := func () error {
		Init()

		privateKey, _ := common.HexStringToBytes("cd5fa29ed5b0e951f3d1bce5997458706186320f1dd89156a73d54ed752a7f37")
		account, err := vault.NewAccountWithPrivatekey(privateKey)
		if err != nil {
			return err
		}

		w := NewWalletSDK(account)

		// Query asset balance for this wallet
		balance, err := w.Balance()
		if err != nil {
			return err
		}
		log.Println("asset balance for this wallet is:", balance.String())

		// Transfer asset to some address
		// This call will fail because a new account has no balance
		address, err := account.ProgramHash.ToAddress()
		if err != nil {
			return err
		}
		txid, err := w.Transfer(address, "100")
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Register name for this wallet
		txid, err = w.RegisterName("somename")
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Delete name for this wallet
		// This call will fail because a new account has no name
		txid, err = w.DeleteName("somename")
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Subscribe to bucket 0 of specified topic for this wallet for next 10 blocks
		txid, err = w.Subscribe("identifier", "topic", 0, 10, "meta")
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}

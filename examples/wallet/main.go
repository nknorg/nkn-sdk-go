package main

import (
	"fmt"
	"log"

	"github.com/nknorg/nkn-sdk-go"
)

func main() {
	err := func() error {
		account, err := nkn.NewAccount(nil)
		if err != nil {
			return err
		}

		w, err := nkn.NewWallet(account, &nkn.WalletConfig{Password: "password"})
		if err != nil {
			return err
		}

		walletJSON, err := w.ToJSON()
		if err != nil {
			return err
		}

		walletFromJSON, err := nkn.WalletFromJSON(walletJSON, &nkn.WalletConfig{Password: "password"})
		if err != nil {
			return err
		}

		log.Println("verify address:", nkn.VerifyWalletAddress(w.Address()) == nil)
		log.Println("verify password:", walletFromJSON.VerifyPassword("password"))

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
		txid, err := w.Transfer(address, "100", nil)
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Register name for this wallet
		// This call will fail because a new account has not enough balance to pay the registration fee
		txid, err = w.RegisterName("somename", nil)
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Transfer name owned by this wallet to another public key
		// This call will fail because a new account has no name
		txid, err = w.TransferName("somename", []byte("recipient public key"), nil)
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Delete name owned by this wallet
		// This call will fail because a new account has no name
		txid, err = w.DeleteName("somename", nil)
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Subscribe to bucket 0 of specified topic for this wallet for next 10 blocks
		txid, err = w.Subscribe("identifier", "topic", 10, "meta", nil)
		if err != nil {
			return err
		}
		log.Println("success:", txid)

		// Open nano pay channel to the specified address for the duration of next 200 blocks
		np, err := w.NewNanoPay(address, "0", 200)
		if err != nil {
			return err
		}
		// Send 100 NKN into channel
		tx, err := np.IncrementAmount("100", "0.1")
		txHash := tx.Hash()
		txHashRef := &txHash
		if err != nil {
			return err
		}
		log.Println("success:", txHashRef.ToHexString())

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}

package ethereum

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfave/cli/v2"
)

var contractCMD = &cli.Command{
	Name:  "contract",
	Usage: "operation about solidity contract",
	Subcommands: []*cli.Command{
		{
			Name:  "deploy",
			Usage: "deploy solidity contract to ethereum chain",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "ether_addr",
					Usage:    "the address of ethereum chain",
					Value:    "http://localhost:8545",
					Required: false,
				},
				&cli.StringFlag{
					Name:     "key_path",
					Usage:    "the ethereum account private key path",
					Required: true,
				},
				&cli.StringFlag{
					Name:     "code_path",
					Usage:    "the path of solidity contract",
					Required: true,
				},
			},
			Action: deploy,
		},
		{
			Name:  "invoke",
			Usage: "invoke solidity contract on ethereum chain",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "ether_addr",
					Usage:    "the address of ethereum chain",
					Value:    "http://localhost:8545",
					Required: false,
				},
				&cli.StringFlag{
					Name:     "key_path",
					Usage:    "the ethereum account private key path",
					Required: true,
				},
				&cli.StringFlag{
					Name:     "abi_path",
					Usage:    "the path of solidity contract abi file",
					Required: true,
				},
			},
			Action: invoke,
		},
	},
}

func deploy(ctx *cli.Context) error {
	etherAddr := ctx.String("ether_addr")
	keyPath := ctx.String("key_path")
	codePath := ctx.String("code_path")

	etherCli, privateKey, err := help(etherAddr, keyPath)
	if err != nil {
		return err
	}

	// compile solidity first
	compileResult, err := compileSolidityCode(codePath)
	if err != nil {
		return err
	}

	if len(compileResult.Abi) == 0 || len(compileResult.Bins) == 0 || len(compileResult.Types) == 0 {
		return fmt.Errorf("empty contract")
	}
	// deploy a contract
	auth := bind.NewKeyedTransactor(privateKey)
	parsed, err := abi.JSON(strings.NewReader(compileResult.Abi[0]))
	if err != nil {
		return err
	}

	for i, bin := range compileResult.Bins {
		addr, _, _, err := bind.DeployContract(auth, parsed, common.FromHex(bin), etherCli)
		if err != nil {
			return err
		}
		fmt.Printf("\n======= %s =======\n", compileResult.Types[i])
		fmt.Printf("Deployed contract address is %s\n", addr.Hex())
		fmt.Printf("Contract JSON ABI\n%s\n", compileResult.Abi[i])
	}

	return nil
}

func invoke(ctx *cli.Context) error {
	etherAddr := ctx.String("ether_addr")
	keyPath := ctx.String("key_path")
	abiPath := ctx.String("abi_path")

	if ctx.NArg() < 2 {
		return fmt.Errorf("invoke contract must include address and function")
	}

	args := ctx.Args().Slice()
	if ctx.NArg() == 2 {
		args = append(args, "")
	}
	dstAddr := args[0]
	function := args[1]
	argAbi := args[2]

	file, err := ioutil.ReadFile(abiPath)
	if err != nil {
		return err
	}

	etherCli, privateKey, err := help(etherAddr, keyPath)
	if err != nil {
		return err
	}

	ab, err := abi.JSON(bytes.NewReader(file))
	if err != nil {
		return err
	}

	etherSession := &EtherSession{
		privateKey: privateKey,
		etherCli:   etherCli,
		ctx:        context.Background(),
		ab:         ab,
	}

	// prepare for invoke parameters
	var argx []interface{}
	if len(args) != 0 {
		argSplits := strings.Split(argAbi, ",")
		var argArr [][]byte
		for _, arg := range argSplits {
			argArr = append(argArr, []byte(arg))
		}

		argx, err = ABIUnmarshal(ab, argArr, function)
		if err != nil {
			return err
		}
	}

	packed, err := ab.Pack(function, argx...)
	if err != nil {
		return err
	}

	invokerAddr := crypto.PubkeyToAddress(privateKey.PublicKey)
	to := common.HexToAddress(dstAddr)

	if ab.Methods[function].IsConstant() {
		// for read only eth calls
		result, err := etherSession.ethCall(&invokerAddr, &to, function, packed)
		if err != nil {
			return err
		}

		if result == nil {
			fmt.Printf("\n======= invoke function %s =======\n", function)
			fmt.Println("no result")
			return nil
		}

		str := ""
		for _, r := range result {
			if r != nil {
				if reflect.TypeOf(r).String() == "[32]uint8" {
					v, ok := r.([32]byte)
					if ok {
						r = string(v[:])
					}
				}
			}
			str = fmt.Sprintf("%s,%v", str, r)
		}

		str = strings.Trim(str, ",")
		fmt.Printf("\n======= invoke function %s =======\n", function)
		fmt.Printf("call result: %s\n", str)
		return nil
	}

	// for write only eth transaction
	receipt, err := etherSession.ethTx(&invokerAddr, &to, packed)
	if err != nil {
		return err
	}

	fmt.Printf("\n======= invoke function %s =======\n", function)
	fmt.Printf("transaction hash: %s\n", receipt.TxHash.Hex())
	fmt.Printf("transaction status: %d\n", receipt.Status)
	return nil
}

func help(etherAddr, keyPath string) (*ethclient.Client, *ecdsa.PrivateKey, error) {
	etherCli, err := ethclient.Dial(etherAddr)
	if err != nil {
		return nil, nil, err
	}

	keyByte, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}

	type EtherAccount struct {
		PrivateKeys map[string]string `json:"private_keys"`
	}
	accounts := &EtherAccount{}
	if err := json.Unmarshal(keyByte, accounts); err != nil {
		return nil, nil, err
	}

	var privateKey *ecdsa.PrivateKey
	for _, account := range accounts.PrivateKeys {
		privateKey, err = crypto.HexToECDSA(account)
		if err != nil {
			return nil, nil, err
		}
		break
	}

	return etherCli, privateKey, nil
}
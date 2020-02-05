package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/bandprotocol/d3n/chain/app"
	"github.com/bandprotocol/d3n/chain/d3nlib"
	"github.com/bandprotocol/d3n/chain/x/zoracle"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gin-gonic/gin"
	"github.com/levigross/grequests"
	"github.com/spf13/viper"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	cmn "github.com/tendermint/tendermint/libs/common"
	rpc "github.com/tendermint/tendermint/rpc/client"

	"github.com/bandprotocol/d3n/chain/wasm"
)

type OracleRequest struct {
	CodeHash cmn.HexBytes    `json:"codeHash" binding:"len=32"`
	Params   json.RawMessage `json:"params" binding:"required"`
}

type OracleRequestResp struct {
	RequestId uint64 `json:"id"`
	TxHash    string `json:"txHash"`
}

type ExecuteRequest struct {
	Code   cmn.HexBytes    `json:"code" binding:"required"`
	Params json.RawMessage `json:"params" binding:"required"`
}

type ExecuteResponse struct {
	Result json.RawMessage `json:"result"`
}

type ParamsInfoRequest struct {
	Code cmn.HexBytes `json:"code" binding:"required"`
}

type ParamsInfoResponse struct {
	Params json.RawMessage `json:"params"`
}

type StoreRequest struct {
	Code cmn.HexBytes `json:"code" binding:"required"`
	Name string       `json:"name" binding:"required"`
}

type StoreResponse struct {
	TxHash   string       `json:"txHash"`
	CodeHash cmn.HexBytes `json:"codeHash"`
}

type Command struct {
	Cmd       string   `json:"cmd"`
	Arguments []string `json:"args"`
}

var allowedCommands = map[string]bool{"curl": true, "date": true}

func execWithTimeout(command Command, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, command.Cmd, command.Arguments...)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return []byte{}, fmt.Errorf("Command timed out")
	}
	if err != nil {
		return []byte{}, err
	}
	return out, nil
}

func getEnv(key, def string) string {
	tmp := os.Getenv(key)
	if tmp == "" {
		return def
	}
	return tmp
}

var (
	port     = getEnv("PORT", "5001")
	nodeURI  = getEnv("NODE_URI", "http://localhost:26657")
	queryURI = getEnv("QUERY_URI", "http://localhost:1317")
	priv     = getEnv("PRIVATE_KEY", "eedda7a96ad35758f2ffc404d6ccd7be913f149a530c70e95e2e3ee7a952a877")
)

var rpcClient *rpc.HTTP
var pk secp256k1.PrivKeySecp256k1
var bandProvider d3nlib.BandProvider
var cdc *codec.Codec

type serializeResponse struct {
	Result cmn.HexBytes `json:"result"`
}

func handleRequestData(c *gin.Context) {
	var req OracleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := grequests.Get(
		fmt.Sprintf(`%s/zoracle/serialize_params/%x`, queryURI, req.CodeHash),
		&grequests.RequestOptions{
			Params: map[string]string{"params": string(req.Params)},
		},
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if resp.StatusCode != 200 {
		var body map[string]interface{}
		err := json.Unmarshal(resp.Bytes(), &body)
		if err == nil {
			c.JSON(resp.StatusCode, body)
		} else {
			c.JSON(resp.StatusCode, resp.Bytes())
		}
		return
	}

	var respParams serializeResponse
	err = resp.JSON(&respParams)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	params := respParams.Result

	// TODO: Replace by `BandStatefulClient` after implementation finished.
	txr, err := bandProvider.SendTransaction(
		[]sdk.Msg{zoracle.NewMsgRequest(req.CodeHash, params, 10, bandProvider.Sender())},
		0, 20000000, "", "", "",
		flags.BroadcastBlock,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	requestId := uint64(0)
	for _, event := range txr.Events {
		if event.Type == "request" {
			for _, attr := range event.Attributes {
				if string(attr.Key) == "id" {
					requestId, err = strconv.ParseUint(attr.Value, 10, 64)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					break
				}
			}
		}
	}
	if requestId == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("cannot find requestId: %v", txr)})
		return
	}

	c.JSON(200, OracleRequestResp{
		RequestId: requestId,
		TxHash:    txr.TxHash,
	})
}

func handleParamsInfo(c *gin.Context) {
	var req ParamsInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	res, err := wasm.ParamsInfo(req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, ParamsInfoResponse{
		Params: res,
	})
}

func handleExecute(c *gin.Context) {
	var req ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rawParams, err := wasm.SerializeParams(req.Code, req.Params)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Call prepare function
	prepare, err := wasm.Prepare(req.Code, rawParams)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var commands []Command
	err = json.Unmarshal(prepare, &commands)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	type queryParallelInfo struct {
		index      int
		answer     string
		httpStatus int
		err        gin.H
	}
	chanQueryParallelInfo := make(chan queryParallelInfo, len(commands))
	for i, command := range commands {
		go func(index int, command Command) {
			info := queryParallelInfo{index: index, answer: "", err: nil}
			if !allowedCommands[command.Cmd] {
				info.httpStatus = http.StatusBadRequest
				info.err = gin.H{"error": fmt.Errorf("handleRequest unknown command %s", command.Cmd)}
				chanQueryParallelInfo <- info
				return
			}
			dockerCommand := Command{
				Cmd: "docker",
				Arguments: append([]string{
					"run", "--rm", "band-provider",
					command.Cmd,
				}, command.Arguments...),
			}
			query, err := execWithTimeout(dockerCommand, 10*time.Second)
			if err != nil {
				info.httpStatus = http.StatusBadRequest
				info.err = gin.H{"error": err.Error()}
				chanQueryParallelInfo <- info
				return
			}

			info.answer = string(query)
			info.httpStatus = http.StatusOK
			chanQueryParallelInfo <- info
		}(i, command)
	}

	answers := make([]string, len(commands))
	for i := 0; i < len(commands); i++ {
		info := <-chanQueryParallelInfo
		if info.err != nil {
			c.JSON(info.httpStatus, info.err)
			return
		}
		answers[info.index] = info.answer
	}

	b, _ := json.Marshal(answers)

	rawResult, err := wasm.Execute(req.Code, rawParams, [][]byte{b})
	result, err := wasm.ParseResult(req.Code, rawResult)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, ExecuteResponse{
		Result: result,
	})
}

func handleStore(c *gin.Context) {
	var req StoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	codeHash := zoracle.NewStoredCode(req.Code, req.Name, bandProvider.Sender()).GetCodeHash()
	// TODO: Replace by `BandStatefulClient` after implementation finished.
	tx, err := bandProvider.SendTransaction(
		[]sdk.Msg{zoracle.NewMsgStoreCode(req.Code, req.Name, bandProvider.Sender())},
		0, 20000000, "", "", "",
		flags.BroadcastBlock,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, StoreResponse{
		TxHash:   tx.TxHash,
		CodeHash: cmn.HexBytes(codeHash),
	})
}

func main() {
	viper.Set("nodeURI", nodeURI)
	privBytes, _ := hex.DecodeString(priv)
	copy(pk[:], privBytes)

	var err error
	bandProvider, err = d3nlib.NewBandProvider(pk)
	if err != nil {
		panic(err)
	}
	cdc = app.MakeCodec()
	rpcClient = rpc.NewHTTP(nodeURI, "/websocket")

	r := gin.Default()
	// Currently gin-contrib/cors not work so add header manually
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
	})

	r.POST("/request", handleRequestData)
	r.POST("/params-info", handleParamsInfo)
	r.POST("/execute", handleExecute)
	r.POST("/store", handleStore)

	r.Run("0.0.0.0:" + port) // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

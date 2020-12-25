package main

import (
	"fmt"
	"strings"

	ws "github.com/gorilla/websocket"
	"github.com/itering/substrate-api-rpc/metadata"
	"github.com/itering/substrate-api-rpc/rpc"
	"github.com/itering/substrate-api-rpc/storage"
	"github.com/itering/substrate-api-rpc/storageKey"
	"github.com/sirupsen/logrus"
)

type EraRewardPoints struct {
	Total       uint32 `json:"total"`
	Individuals []struct {
		AccountId   string `json:"col1"`
		RewardPoint uint32 `json:"col2"`
	} `json:"individual"`
}

func prepareMetadata(conn *ws.Conn) error {
	response, err := sendWsRequest(conn, rpc.StateGetMetadata(0))
	if err != nil {
		return err
	}

	encoded := response.Result.(string)

	metadata.Latest(&metadata.RuntimeRaw{
		Spec: metadataSpecID,
		Raw:  strings.TrimPrefix(encoded, "0x"),
	})

	return nil
}

func sendWsRequest(conn *ws.Conn, data []byte) (*rpc.JsonRpcResult, error) {
	logrus.Tracef("RPC call: %s", data)
	v := &rpc.JsonRpcResult{}

	if err := conn.WriteMessage(ws.TextMessage, data); err != nil {
		return nil, fmt.Errorf("conn.WriteMessage: %w", err)
	}

	if err := conn.ReadJSON(v); err != nil {
		return nil, fmt.Errorf("conn.ReadJSON: %w", err)
	}

	logrus.Tracef("RPC raw result: %+v", v)

	if v.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", v.Error.Message)
	}

	return v, nil
}

func readStorage(conn *ws.Conn, storageSection string, storageMethod string, args ...string) (storage.StateStorage, error) {
	nilStorage := storage.StateStorage("")

	storageKey := storageKey.EncodeStorageKey(storageSection, storageMethod, args...)
	logrus.Tracef("Encoded storage key: %s", storageKey)

	rpcRequest := rpc.StateGetStorage(0, "0x"+storageKey.EncodeKey, "")
	rpcResponse, err := sendWsRequest(conn, rpcRequest)
	if err != nil {
		return nilStorage, err
	}

	// encoded := "0x0422011400268549af4000660482df1ae246dd37bbd89ec67cd4c597fafc8d2f57f5966fa59575e35f00000000d4a4af0000000000e9b54a47e3f401d37798fc4e22f14b78475c2afc64bcd9e46144f693b5a87d9adb1dc8b8f137d8ff8c04811cf57f0c3570a53fd61dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d4934708363694152c706ebfc1b753bc4205f21cadbcac968aa735096b7b0f1ad7841e4674534a37c9eb708a7ffac7d5ba8d6aef49eb71393d663483e304baa09cfeafda9ac75574444cf221a08d823ae304800d58225001e4480c4e10a5e2518445a207278061240062371c112a8fa94019d0bd33098621c4100b56894088cd0cc9f5e84e342c0a7126065647bdc9c1b31d202070ff241000188042f80430622740c0e25113780015c0b635d415c30c28c2f021881b17f0d83c501a7f94e8037cbb491574a550490a25e3c4a025835a0a6ad8208c10800118190155dc49d0a1b97a035c98804b08ec3126466c928527a2db1483d00317081d00a48234f158a6693048226049359ec19f224500011410813e875a09c6215e9a02802d6310ab1821a26a2065a021fb6c100ceb77ad90349e6007b803b6b06408cb60a403c3924dc00a2ae637ef1bc0bd0000000000000000000000000000000000000000000000000000000000ace3bd0000000000000000000000000000000000000000000000000000000000e0be593b22200d000000000000000000000000000000000000000000000000000884a0adde9c52eb3b838c988c6596e0656317e30e20b9d53bfaf327da5c510435e33024880d939e4e5dd6352d01123d46228ee839a6e19960b83a45085922197c27ea88c6d06340f85b4985310cbe45521c64112b147e0c7bb85ae59c10a19ffc0d2233186e72ebe087fe6665bb0000"
	encoded, ok := rpcResponse.Result.(string)
	if !ok {
		return nilStorage, fmt.Errorf("unable to parse storage %s.%s, raw result: %+v", storageSection, storageMethod, rpcResponse.Result)
	}

	if encoded == "" {
		return nilStorage, nil
	}

	storage, err := storage.Decode(encoded, storageKey.ScaleType, nil)
	logrus.Tracef("Decoded storage: %v", storage)

	return storage, err
}

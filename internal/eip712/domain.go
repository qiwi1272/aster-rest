package eip712

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const USER_AGENT = "epi712-go/1.0.0dev"

type Client struct {
	http.Client
	BaseURL string

	DomainHash hexutil.Bytes // precompute for this library, used for signing/verifying all messages
	Types      *apitypes.TypedData
}

func (ec *Client) TypesInit(domain apitypes.TypedDataDomain, knownTypes apitypes.Types) error {
	// hardcode EPI 712 domain type
	knownTypes["EIP712Domain"] = []apitypes.Type{
		{Name: "name", Type: "string"},
		{Name: "version", Type: "string"},
		{Name: "chainId", Type: "uint256"},
		{Name: "verifyingContract", Type: "address"},
	}

	types := &apitypes.TypedData{
		Types:  knownTypes,
		Domain: domain,
	}

	domainHash, err := types.HashStruct("EIP712Domain", types.Domain.Map())
	if err != nil {
		return err
	}
	ec.DomainHash = domainHash
	ec.Types = types
	return nil
}

func (ec *Client) GetDomainHash() hexutil.Bytes {
	return ec.DomainHash
}

func (ec *Client) GetTypes() *apitypes.TypedData {
	return ec.Types
}

// generic request function for http client
func (ec *Client) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	var b []byte
	var err error
	var req *http.Request

	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err = http.NewRequestWithContext(ctx, method, ec.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	resp, err = ec.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	_, err = out.ReadFrom(resp.Body) // inplace, discard bytes length

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http client error %d: %s", resp.StatusCode, out.String())
	}

	return out.Bytes(), err
}

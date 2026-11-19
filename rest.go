package asterRest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qiwi1272/aster-rest/internal/eip712"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const SpotRestUrl = "https://sapi.asterdex.com"
const FutsRestUrl = "https://fapi.asterdex.com"

type Rest struct {
	Client  *eip712.Client
	Types   *apitypes.TypedData
	account string
	signer  *eip712.Signer
	Before  func(context.Context, string, string) error
	Observe func(RequestResult)
}

func NewRest(spot bool, account, key string) (*Rest, error) {
	var url string
	if spot {
		url = SpotRestUrl + "/api/v3"
	} else {
		url = FutsRestUrl + "/fapi/v3"
	}

	aster := &Rest{
		Client: &eip712.Client{
			BaseURL: url,
		},
		account: account,
	}

	if len(key) != 0 {
		signer, err := eip712.NewSigner(key)
		if err != nil {
			return nil, err
		}
		aster.signer = signer
	}

	// set http conf
	aster.Client.Transport = &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ExpectContinueTimeout: 0,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
	}
	aster.Client.Timeout = 10 * time.Second

	// init static eip 712 data
	err := aster.Client.TypesInit(
		apitypes.TypedDataDomain{
			Name:              "AsterSignTransaction",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(1666),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		apitypes.Types{
			"Message": []apitypes.Type{
				{Name: "msg", Type: "string"},
			},
		})
	aster.Client.Types.PrimaryType = "Message"
	aster.Client.Types.Message = map[string]interface{}{
		"msg": "$msg",
	}

	aster.Types = aster.Client.Types

	return aster, err
}

// nextNonce returns a strictly increasing microsecond nonce. Two calls inside
// the same microsecond would otherwise produce the same value and the second
// request gets rejected as a replay.
var nonceMu sync.Mutex
var signerNonces = map[string]int64{}

func (a *Rest) getNonce() int64 {
	nonceMu.Lock()
	defer nonceMu.Unlock()
	key := a.signer.Address
	n := time.Now().UnixMicro()
	if n <= signerNonces[key] {
		n = signerNonces[key] + 1
	}
	signerNonces[key] = n
	return n
}

// RequestSigned performs an authenticated v3 request.
//
// GET carries params in the query string; every other method sends them as a
// form-encoded body. A JSON body is silently ignored by the API — it comes
// back as "mandatory parameter not sent" for fields you did include.
//
// Errors are classed as described in errors.go: not sent, unknown, or rejected.
func (a *Rest) RequestSigned(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if a.signer == nil {
		return nil, fmt.Errorf("%w: signed request without a signer key", ErrNotSent)
	}
	// The rate-limit hold comes first, so a request that cannot go doesn't reach Before.
	if err := holdErr(a.Client.BaseURL); err != nil {
		return nil, err
	}
	if a.Before != nil {
		if err := a.Before(ctx, method, path); err != nil {
			return nil, err
		}
	}
	// Never mutate caller-owned parameters: requests can be retried or concurrent.
	cloned := url.Values{}
	for k, v := range params {
		cloned[k] = append([]string(nil), v...)
	}
	params = cloned

	params.Set("user", a.account)
	params.Set("signer", a.signer.Address)
	params.Set("nonce", strconv.FormatInt(a.getNonce(), 10))

	message := apitypes.TypedDataMessage{
		"msg": params.Encode(),
	}

	hash, err := eip712.HashEIP712Message(message, "Message", a.Client)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}

	signature, err := a.signer.SignMessage(hash)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	params.Set("signature", signature)
	return a.send(ctx, method, path, params)
}

// RequestPublic performs an unsigned GET (market data, exchange info). It obeys the same
// rate-limit hold as signed requests, since the venue's limits are per IP.
func (a *Rest) RequestPublic(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if err := holdErr(a.Client.BaseURL); err != nil {
		return nil, err
	}
	return a.send(ctx, http.MethodGet, path, params)
}

// send dispatches one request and classes the outcome.
func (a *Rest) send(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	encoded := params.Encode()
	method = strings.ToUpper(method)
	var req *http.Request
	var err error
	if method == http.MethodGet {
		u := a.Client.BaseURL + path
		if encoded != "" {
			u += "?" + encoded
		}
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, a.Client.BaseURL+path, strings.NewReader(encoded))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSent, err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", eip712.USER_AGENT)

	started := time.Now()
	resp, err := a.Client.Do(req)
	if err != nil {
		result := RequestResult{Method: method, Path: path, Started: started, Elapsed: time.Since(started), Unknown: true}
		if a.Observe != nil {
			a.Observe(result)
		}
		return nil, &APIError{Unknown: true, Message: "transport failure", Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
		setHold(a.Client.BaseURL, resp.Header.Get("Retry-After"))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result := RequestResult{Method: method, Path: path, Started: started, Elapsed: time.Since(started), Status: resp.StatusCode, Header: resp.Header.Clone(), Unknown: true}
		if a.Observe != nil {
			a.Observe(result)
		}
		return nil, &APIError{Status: resp.StatusCode, Unknown: true, Message: "incomplete response body", Cause: err}
	}
	var api APIError
	_ = json.Unmarshal(body, &api)
	// -1006 and -1007 are the venue saying it lost track of the request (unexpected backend
	// reply, backend timeout): it may have executed.
	unknown := resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || api.Code == -1006 || api.Code == -1007
	result := RequestResult{Method: method, Path: path, Started: started, Elapsed: time.Since(started), Status: resp.StatusCode, Header: resp.Header.Clone(), Unknown: unknown}
	if a.Observe != nil {
		a.Observe(result)
	}
	if resp.StatusCode != http.StatusOK || api.Code < 0 {
		api.Status = resp.StatusCode
		api.Unknown = unknown
		if api.Message == "" {
			api.Message = strings.TrimSpace(string(body[:min(len(body), 200)]))
		}
		api.Message = redact(api.Message, params)
		return nil, &api
	}
	return body, nil
}

func (a *Rest) Ping(ctx context.Context) ([]byte, error) {
	return a.RequestPublic(ctx, "/ping", nil)
}

func (a *Rest) ExchangeInfo(ctx context.Context) (exInfo ExchangeInfo, err error) {
	var resp []byte
	resp, err = a.RequestPublic(ctx, "/exchangeInfo", nil)
	if err != nil {
		return exInfo, err
	}
	if err := json.Unmarshal(resp, &exInfo); err != nil {
		return exInfo, err
	}
	return exInfo, nil
}

func (a *Rest) TickerPrice(ctx context.Context, base, quote string) (px TickerPrice, err error) {
	var resp []byte
	resp, err = a.RequestPublic(ctx, "/ticker/price", url.Values{"symbol": {base + quote}})
	if err != nil {
		return px, err
	}
	if err := json.Unmarshal(resp, &px); err != nil {
		return px, err
	}
	return px, nil
}

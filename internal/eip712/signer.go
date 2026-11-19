package eip712

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	"crypto/ecdsa"
)

type Signer struct {
	privateKey *ecdsa.PrivateKey
	Address    string
}

func NewSigner(key string) (*Signer, error) {
	strip := strings.TrimPrefix(key, "0x")

	signer := &Signer{}

	var err error
	signer.privateKey, err = crypto.HexToECDSA(strip)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	signer.Address = crypto.PubkeyToAddress(signer.privateKey.PublicKey).Hex()

	return signer, nil
}

func (s *Signer) GetPk() *ecdsa.PrivateKey {
	return s.privateKey
}

func (s *Signer) sign(messageHash []byte) (string, error) {
	sig, err := crypto.Sign(messageHash, s.privateKey)
	if err != nil {
		return "", err
	}
	sig[64] += 27 // recovery byte fix  |  much love to _0xmer :)

	return "0x" + hex.EncodeToString(sig), nil
}

type EIP712Client interface {
	GetTypes() *apitypes.TypedData
	GetDomainHash() hexutil.Bytes
}

func eip712Hash(domainHash []byte, messageHash []byte) []byte {
	fullHash := make([]byte, 0, 66)
	fullHash = append(fullHash, 0x19, 0x01)
	fullHash = append(fullHash, domainHash...)
	fullHash = append(fullHash, messageHash...)
	return crypto.Keccak256(fullHash)
}

func HashEIP712Message(msg apitypes.TypedDataMessage, msgType string, client EIP712Client) (hexutil.Bytes, error) {
	messageHash, err := client.GetTypes().HashStruct(msgType, msg)
	if err != nil {
		return nil, err
	}

	return eip712Hash(client.GetDomainHash(), messageHash), nil
}

func (s *Signer) SignMessage(bytes hexutil.Bytes) (string, error) {
	return s.sign(bytes)
}

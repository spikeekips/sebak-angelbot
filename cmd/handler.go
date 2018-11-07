package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	"github.com/stellar/go/keypair"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/errors"
	"boscoin.io/sebak/lib/network/httputils"
	"boscoin.io/sebak/lib/transaction"
	"boscoin.io/sebak/lib/transaction/operation"
)

const (
	defaultWaitTimeout time.Duration = 60 * time.Second
)

type Handler struct {
	kp            *keypair.Full
	sebakEndpoint *common.Endpoint
	networkID     []byte
}

func getHTTP2Client() *common.HTTP2Client {
	h2c, _ := common.NewHTTP2Client(
		3*time.Second,
		3*time.Second,
		false,
	)

	return h2c
}

func (h *Handler) sendMessage(method, path string, message []byte) (b []byte, err error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	u := (*url.URL)(h.sebakEndpoint).ResolveReference(&url.URL{Path: path})

	var response *http.Response
	if method == "GET" {
		if response, err = getHTTP2Client().Get(u.String(), headers); err != nil {
			if verbose {
				log.Debug("failed to get request", "error", err)
			}
			return
		}
	} else {
		if response, err = getHTTP2Client().Post(u.String(), message, headers); err != nil {
			if verbose {
				log.Debug("failed to post request", "error", err)
			}
			return
		}
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		if verbose {
			log.Debug("failed to get response", "error", response)
		}
		err = errors.BlockAccountDoesNotExists
		return
	}

	if b, err = ioutil.ReadAll(response.Body); err != nil {
		if verbose {
			log.Debug("failed to read response body", "error", err)
		}
		return
	}

	return
}

func (h *Handler) getAccount(address string) (ba *block.BlockAccount, err error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	var retBody []byte
	if retBody, err = h.sendMessage("GET", "/api/v1/accounts/"+address, []byte{}); err != nil {
		err = errors.BlockAccountDoesNotExists
	}

	if err = json.Unmarshal(retBody, &ba); err != nil {
		if verbose {
			log.Debug("failed to load BlockAccount", "error", err)
		}
		return
	}

	return
}

func (h *Handler) createAccount(w http.ResponseWriter, ba *block.BlockAccount, address string, balance common.Amount, timeout time.Duration) (
	baCreated *block.BlockAccount,
	err error,
) {
	// send tx for create-account
	opb := operation.NewCreateAccount(address, balance, "")
	op := operation.Operation{
		H: operation.Header{
			Type: operation.TypeCreateAccount,
		},
		B: opb,
	}

	txBody := transaction.Body{
		Source:     kp.Address(),
		Fee:        common.Amount(common.BaseFee),
		SequenceID: ba.SequenceID,
		Operations: []operation.Operation{op},
	}

	tx := transaction.Transaction{
		H: transaction.Header{
			Created: common.NowISO8601(),
			Hash:    txBody.MakeHashString(),
		},
		B: txBody,
	}
	tx.Sign(kp, h.networkID)

	var body []byte
	if body, err = tx.Serialize(); err != nil {
		log.Debug("failed to write the response body", "error", err)
		return
	}
	log.Debug("trying to send transaction", "hash", tx.GetHash())

	if _, err = h.sendMessage("POST", "/node/message", body); err != nil {
		return
	}

	cn, ok := w.(http.CloseNotifier)
	if !ok {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	timer := time.NewTimer(timeout)

	log.Debug("checking new account", "address", address, "hash", tx.GetHash())
	var created bool
	for {
		select {
		case <-cn.CloseNotify():
			goto End
		case <-timer.C:
			goto End
		default:
			time.Sleep(900 * time.Millisecond)

			// check BlockTransactionHistory
			if baCreated, err = h.getAccount(address); err == nil {
				if baCreated.Balance != balance {
					err = fmt.Errorf("failed to create account")
					log.Error(
						"failed to create new account, balance mismatch",
						"address", address,
						"created balance", baCreated.Balance,
						"expected balance", balance,
						"hash", tx.GetHash(),
					)
				} else {
					created = true
					log.Debug("new account is created successfully", "address", address, "hash", tx.GetHash())
				}

				goto End
			}
		}
	}

End:
	if created {
		err = nil
		return
	}

	err = fmt.Errorf("failed to create account: hash=%s", tx.GetHash())
	return
}

func (h *Handler) accountHandler(w http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]

	var err error

	// balance
	balance := common.BaseReserve
	if balanceString, found := r.URL.Query()["balance"]; found && len(balanceString) > 0 && len(balanceString[0]) > 0 {
		if balance, err = common.AmountFromString(balanceString[0]); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if balance < common.BaseReserve {
		httputils.WriteJSONError(w, errors.OperationAmountUnderflow)
		return
	}

	// timeout
	timeout := defaultWaitTimeout
	if timeoutString, found := r.URL.Query()["timeout"]; found && len(timeoutString) > 0 && len(timeoutString[0]) > 0 {
		if timeout, err = time.ParseDuration(timeoutString[0]); err != nil {
			httputils.WriteJSONError(w, fmt.Errorf("invalid timeout format"))
			return
		}
	}

	// check address is valid
	var parsedKP keypair.KP
	if parsedKP, err = keypair.Parse(address); err != nil {
		httputils.WriteJSONError(w, err)
		return
	} else if _, ok := parsedKP.(*keypair.Full); ok {
		httputils.WriteJSONError(w, fmt.Errorf("don't provide secret seed; PLEASE!!!"))
		return
	}

	// check account exists
	if _, err = h.getAccount(address); err == nil {
		http.Error(w, "account is already exists", http.StatusBadRequest)
		httputils.WriteJSONError(w, errors.BlockAccountAlreadyExists)
		return
	}

	var baMaster *block.BlockAccount
	if baMaster, err = h.getAccount(kp.Address()); err != nil {
		log.Debug("failed to get master account", "address", kp.Address())
		httputils.WriteJSONError(w, errors.BlockAccountDoesNotExists)
		return
	}

	var baCreated *block.BlockAccount
	if baCreated, err = h.createAccount(w, baMaster, address, balance, timeout); err != nil {
		log.Error(err.Error(), "address", address)
		httputils.WriteJSONError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)

	var body []byte
	if body, err = common.JSONMarshalIndent(baCreated); err != nil {
		log.Debug("failed to serialize BlockAccount", "error", err)
		httputils.WriteJSONError(w, err)
		return
	}

	w.Write(append(body, []byte("\n")...))
}

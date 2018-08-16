package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/stellar/go/keypair"

	"boscoin.io/sebak/lib"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/error"
)

const (
	baseBalance        int           = 1000000000000
	defaultWaitTimeout time.Duration = 60 * time.Second
)

type Handler struct {
	kp            *keypair.Full
	sebakEndpoint *sebakcommon.Endpoint
	networkID     []byte
}

func getHTTP2Client() *sebakcommon.HTTP2Client {
	h2c, _ := sebakcommon.NewHTTP2Client(
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
		err = sebakerror.ErrorBlockAccountDoesNotExists
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

func (h *Handler) getAccount(address string) (ba *sebak.BlockAccount, err error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	var retBody []byte
	if retBody, err = h.sendMessage("GET", "/api/account/"+address, []byte{}); err != nil {
		err = sebakerror.ErrorBlockAccountDoesNotExists
	}

	if err = json.Unmarshal(retBody, &ba); err != nil {
		if verbose {
			log.Debug("failed to load BlockAccount", "error", err)
		}
		return
	}

	return
}

func (h *Handler) createAccount(w http.ResponseWriter, ba *sebak.BlockAccount, address string, balance sebak.Amount, timeout time.Duration) (
	baCreated *sebak.BlockAccount,
	err error,
) {
	// send tx for create-account
	opb := sebak.NewOperationBodyCreateAccount(address, balance)
	op := sebak.Operation{
		H: sebak.OperationHeader{
			Type: sebak.OperationCreateAccount,
		},
		B: opb,
	}

	txBody := sebak.TransactionBody{
		Source:     kp.Address(),
		Fee:        sebak.Amount(sebak.BaseFee),
		Checkpoint: ba.Checkpoint,
		Operations: []sebak.Operation{op},
	}

	tx := sebak.Transaction{
		T: "transaction",
		H: sebak.TransactionHeader{
			Created: sebakcommon.NowISO8601(),
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
				if sebak.MustAmountFromString(baCreated.Balance) != balance {
					err = errors.New("failed to create account")
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
	balance := sebak.Amount(baseBalance)
	if balanceString, found := r.URL.Query()["balance"]; found && len(balanceString) > 0 && len(balanceString[0]) > 0 {
		if balance, err = sebak.AmountFromString(balanceString[0]); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// timeout
	timeout := defaultWaitTimeout
	if timeoutString, found := r.URL.Query()["timeout"]; found && len(timeoutString) > 0 && len(timeoutString[0]) > 0 {
		if u, err := strconv.ParseUint(timeoutString[0], 10, 64); err != nil {
			http.Error(w, "invalid `timeout`", http.StatusBadRequest)
			return
		} else {
			timeout = time.Duration(u) * time.Millisecond
		}
	}

	// check address is valid
	var parsedKP keypair.KP
	if parsedKP, err = keypair.Parse(address); err != nil {
		http.Error(w, "found invalid address", http.StatusBadRequest)
		return
	} else if _, ok := parsedKP.(*keypair.Full); ok {
		http.Error(w, "don't provide secret seed; PLEASE!!!", http.StatusBadRequest)
		return
	}

	// check account exists
	if _, err = h.getAccount(address); err == nil {
		log.Debug("account is already exists")
		http.Error(w, "account is already exists", http.StatusBadRequest)
		return
	}

	var baMaster *sebak.BlockAccount
	if baMaster, err = h.getAccount(kp.Address()); err != nil {
		log.Debug("failed to get master account")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusBadRequest)
		return
	}

	var baCreated *sebak.BlockAccount
	if baCreated, err = h.createAccount(w, baMaster, address, balance, timeout); err != nil {
		log.Error(err.Error(), "address", address)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)

	var body []byte
	if body, err = baCreated.Serialize(); err != nil {
		log.Debug("failed to serialize BlockAccount", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusBadRequest)
		return
	}

	_, err = w.Write(body)
	if err != nil {
		log.Debug("failed to write the response body", "error", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
}
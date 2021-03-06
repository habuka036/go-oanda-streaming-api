package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var baseUrl string

// {
// 	"time": "2016-12-20T05:55:46.064294036Z",
// 	"type": "HEARTBEAT"
// }

// {
// 	"asks": [
// 		{
// 			"liquidity": 10000000,
// 			"price": "117.680"
// 		},
// 		{
// 			"liquidity": 10000000,
// 			"price": "117.682"
// 		}
// 	],
// 	"bids": [
// 		{
// 			"liquidity": 10000000,
// 			"price": "117.665"
// 		},
// 		{
// 			"liquidity": 10000000,
// 			"price": "117.663"
// 		}
// 	],
// 	"closeoutAsk": "117.684",
// 	"closeoutBid": "117.661",
// 	"instrument": "USD_JPY",
// 	"status": "tradeable",
// 	"time": "2016-12-20T05:55:35.676011610Z",
// 	"type": "PRICE"
// }

type Tick struct {
	Asks        []Quote `json:"asks"`
	Bids        []Quote `json:"bids"`
	CloseoutAsk string  `json:"closeoutAsk"`
	CloseoutBid string  `json:"closeoutBid"`
	Instrument  string  `json:"instrument"`
	Status      string  `json:"status"`
	Time        string  `json:"time,omitempty"`
	Type        string  `json:"type"`

	// used to avoid parsing the Time multiple times
	parsedTime time.Time
}

func (t *Tick) IsJapanese() bool {
	return strings.Contains(t.Instrument, "JPY")
}

func (t *Tick) IsHeartbeat() bool {
	return "HEARTBEAT" == t.Type
}

func (t *Tick) IsTradeable() bool {
	return "tradeable" == t.Status
}

func (t *Tick) Symbol() string {
	return strings.Replace(t.Instrument, "_", "", 1)
}

func (t *Tick) parseTime() (time.Time, error) {
	if !t.parsedTime.IsZero() {
		return t.parsedTime, nil
	}

	parsedTime, err := time.Parse(time.RFC3339Nano, t.Time)
	if err != nil {
		return parsedTime, err
	}

	t.parsedTime = parsedTime

	return t.parsedTime, nil
}

func (t *Tick) UnixTimestamp() (int64, error) {
	parsedTime, err := t.parseTime()
	if err != nil {
		return int64(0), err
	}

	return parsedTime.Unix(), nil
}

func (t *Tick) Nanoseconds() (int64, error) {
	parsedTime, err := t.parseTime()
	if err != nil {
		return int64(0), err
	}

	return int64(parsedTime.Nanosecond()), nil
}

func (t *Tick) BestAsk() (float64, error) {
	if 0 == len(t.Asks) {
		return 0.0, nil
	}

	var best float64

	// best ask is the lowest
	for _, ask := range t.Asks {
		val, err := ask.PriceAsFloat()
		if err != nil {
			return 0.0, err
		}
		if 0 == best {
			best = val
		} else if val < best {
			best = val
		}
	}

	return best, nil
}

func (t *Tick) BestBid() (float64, error) {
	if 0 == len(t.Bids) {
		return 0.0, nil
	}

	var best float64

	// best bid is the highest
	for _, bid := range t.Bids {
		val, err := bid.PriceAsFloat()
		if err != nil {
			return 0.0, err
		}
		if val > best {
			best = val
		}
	}

	return best, nil
}

type Quote struct {
	Liquidity float64  `json:"liquidity"`
	Price     string `json:"price"`
}

func (q *Quote) PriceAsFloat() (float64, error) {
	val, err := strconv.ParseFloat(q.Price, 64)
	if err != nil {
		return float64(0), err
	}

	return val, nil
}

type Client struct {
	account    string
	token      string
	currencies string
}

func New(account, token, currencies string, live bool) *Client {
	if live {
		baseUrl = "https://stream-fxtrade.oanda.com/v3/accounts/%s/pricing/stream?instruments=%s"
	} else {
		baseUrl = "https://stream-fxpractice.oanda.com/v3/accounts/%s/pricing/stream?instruments=%s"
	}
	return &Client{
		account:    account,
		token:      token,
		currencies: currencies,
	}
}

func (c *Client) url() string {
	return fmt.Sprintf(baseUrl, c.account, c.currencies)
}

func (c *Client) Run(f func(*Tick)) error {
	req, err := http.NewRequest("GET", c.url(), nil)
	if err != nil {
		return errors.New(fmt.Sprint("http.NewRequest:", err))
	}

	// set our bearer token
	req.Header.Set("Authorization", "Bearer "+c.token)

	// just use the DefaultClient, no need to be fancy here
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New(fmt.Sprint("http.Get:", err))
	}

	tick := &Tick{}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// technically, we should never get io.EOF here
			return errors.New(fmt.Sprint("reader.ReadBytes:", err))
		}

		if err := json.Unmarshal(line, tick); err != nil {
			return errors.New(fmt.Sprint("json.Unmarshal:", err))
		}

		// skip a few kinds of ticks here:
		//   - the heartbeat which is sent every 5 seconds
		//   - the "last prices" sent when initially connecting to the API
		if tick.IsTradeable() {
			f(tick)
		}
	}
}

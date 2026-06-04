package openrtb

import "encoding/json"

type BidRequest struct {
	ID     string          `json:"id"`
	Imp    []Impression    `json:"imp"`
	TMax   int             `json:"tmax,omitempty"`
	AT     int             `json:"at,omitempty"`
	Cur    []string        `json:"cur,omitempty"`
	Site   *Site           `json:"site,omitempty"`
	App    *App            `json:"app,omitempty"`
	Device *Device         `json:"device,omitempty"`
	User   *User           `json:"user,omitempty"`
	Regs   map[string]any  `json:"regs,omitempty"`
	Source map[string]any  `json:"source,omitempty"`
	BAdv   []string        `json:"badv,omitempty"`
	BCat   []string        `json:"bcat,omitempty"`
	Ext    map[string]any  `json:"ext,omitempty"`
	Raw    json.RawMessage `json:"-"`
}

type Impression struct {
	ID          string         `json:"id"`
	TagID       string         `json:"tagid,omitempty"`
	Secure      int            `json:"secure,omitempty"`
	BidFloor    float64        `json:"bidfloor,omitempty"`
	BidFloorCur string         `json:"bidfloorcur,omitempty"`
	Banner      *Banner        `json:"banner,omitempty"`
	Video       *Video         `json:"video,omitempty"`
	Audio       *Audio         `json:"audio,omitempty"`
	Native      *Native        `json:"native,omitempty"`
	PMP         *PMP           `json:"pmp,omitempty"`
	Ext         map[string]any `json:"ext,omitempty"`
}

type Banner struct {
	W     int      `json:"w,omitempty"`
	H     int      `json:"h,omitempty"`
	Mimes []string `json:"mimes,omitempty"`
}

type Video struct {
	Mimes       []string `json:"mimes,omitempty"`
	MinDuration int      `json:"minduration,omitempty"`
	MaxDuration int      `json:"maxduration,omitempty"`
	Protocols   []int    `json:"protocols,omitempty"`
	W           int      `json:"w,omitempty"`
	H           int      `json:"h,omitempty"`
}

type Audio struct {
	Mimes       []string `json:"mimes,omitempty"`
	MinDuration int      `json:"minduration,omitempty"`
	MaxDuration int      `json:"maxduration,omitempty"`
	Protocols   []int    `json:"protocols,omitempty"`
}

type Native struct {
	Request string `json:"request,omitempty"`
}

type PMP struct {
	PrivateAuction int    `json:"private_auction,omitempty"`
	Deals          []Deal `json:"deals,omitempty"`
}

type Deal struct {
	ID          string  `json:"id"`
	BidFloor    float64 `json:"bidfloor,omitempty"`
	BidFloorCur string  `json:"bidfloorcur,omitempty"`
	AT          int     `json:"at,omitempty"`
}

type Site struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name,omitempty"`
	Domain  string         `json:"domain,omitempty"`
	Page    string         `json:"page,omitempty"`
	Cat     []string       `json:"cat,omitempty"`
	Content map[string]any `json:"content,omitempty"`
	Ext     map[string]any `json:"ext,omitempty"`
}

type App struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	Bundle   string         `json:"bundle,omitempty"`
	StoreURL string         `json:"storeurl,omitempty"`
	Cat      []string       `json:"cat,omitempty"`
	Ext      map[string]any `json:"ext,omitempty"`
}

type Device struct {
	UA         string         `json:"ua,omitempty"`
	IP         string         `json:"ip,omitempty"`
	IFA        string         `json:"ifa,omitempty"`
	LMT        int            `json:"lmt,omitempty"`
	DNT        int            `json:"dnt,omitempty"`
	OS         string         `json:"os,omitempty"`
	Make       string         `json:"make,omitempty"`
	Model      string         `json:"model,omitempty"`
	DeviceType int            `json:"devicetype,omitempty"`
	Geo        map[string]any `json:"geo,omitempty"`
	Ext        map[string]any `json:"ext,omitempty"`
}

type User struct {
	ID      string         `json:"id,omitempty"`
	BuyerID string         `json:"buyeruid,omitempty"`
	Ext     map[string]any `json:"ext,omitempty"`
}

type BidResponse struct {
	ID      string         `json:"id"`
	SeatBid []SeatBid      `json:"seatbid,omitempty"`
	Cur     string         `json:"cur,omitempty"`
	Ext     map[string]any `json:"ext,omitempty"`
}

type SeatBid struct {
	Seat string `json:"seat,omitempty"`
	Bid  []Bid  `json:"bid"`
}

type Bid struct {
	ID      string         `json:"id"`
	ImpID   string         `json:"impid"`
	Price   float64        `json:"price"`
	AdID    string         `json:"adid,omitempty"`
	CID     string         `json:"cid,omitempty"`
	CrID    string         `json:"crid"`
	Adomain []string       `json:"adomain"`
	DealID  string         `json:"dealid,omitempty"`
	AdM     string         `json:"adm,omitempty"`
	NURL    string         `json:"nurl,omitempty"`
	BURL    string         `json:"burl,omitempty"`
	LURL    string         `json:"lurl,omitempty"`
	W       int            `json:"w,omitempty"`
	H       int            `json:"h,omitempty"`
	Ext     map[string]any `json:"ext,omitempty"`
}

func (i Impression) MediaType() string {
	switch {
	case i.Video != nil:
		return "video"
	case i.Audio != nil:
		return "audio"
	case i.Native != nil:
		return "native"
	case i.Banner != nil:
		return "display"
	default:
		return ""
	}
}

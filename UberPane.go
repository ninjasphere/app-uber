package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ninjasphere/gestic-tools/go-gestic-sdk"
	"github.com/ninjasphere/go-ninja/api"
	"github.com/ninjasphere/go-ninja/config"
	"github.com/ninjasphere/go-ninja/model"
	"github.com/ninjasphere/go-uber"
	"github.com/ninjasphere/sphere-go-led-controller/fonts/O4b03b"
	"github.com/ninjasphere/sphere-go-led-controller/util"
)

var uberProduct = config.MustString("uber.product")

var latitude = config.Float(0.0, "latitude")
var longitude = config.Float(0.0, "longitude")

var tapInterval = config.MustDuration("uber.tapInterval")
var updateOnTap = config.MustBool("uber.updateOnTap")
var introDuration = config.MustDuration("uber.introDuration")
var visibleTimeout = config.MustDuration("uber.visibilityTimeout") // Time between frames rendered before we reset the ui.
var updateInterval = config.MustDuration("uber.updateInterval")
var staleDataTimeout = config.MustDuration("uber.staleDataTimeout")

var timezone *time.Location

var imageSurge = util.LoadImage(util.ResolveImagePath("surge.gif"))
var imageNoSurge = util.LoadImage(util.ResolveImagePath("no_surge.gif"))
var imageLogo = util.LoadImage(util.ResolveImagePath("logo.png"))

var confirmTimeout = config.MustDuration("uber.request.confirmTimeout")

var requestImages map[string]util.Image

func loadRequestImages() {
	files, err := ioutil.ReadDir("./images/request_states")

	if err != nil {
		panic("Couldn't load request state images: " + err.Error())
	}

	requestImages = make(map[string]util.Image)

	for _, f := range files {

		if strings.HasSuffix(f.Name(), ".gif") || strings.HasSuffix(f.Name(), ".png") {
			name := strings.TrimSuffix(strings.TrimSuffix(f.Name(), ".png"), ".gif")

			log.Infof("Found state image: " + name)
			requestImages[name] = util.LoadImage(util.ResolveImagePath("request_states/" + f.Name()))
		}

	}
}

type UberPane struct {
	siteModel *ninja.ServiceClient
	site      *model.Site

	times  []*uber.Time
	prices []*uber.Price

	lastTap       time.Time
	lastDoubleTap time.Time

	intro        bool
	introTimeout *time.Timer

	visible        bool
	visibleTimeout *time.Timer

	staleDataTimeout *time.Timer
	updateTimer      *time.Timer

	keepAwake        bool
	keepAwakeTimeout *time.Timer

	requestPane *RequestPane
}

func NewUberPane(conn *ninja.Connection) *UberPane {

	pane := &UberPane{
		siteModel: conn.GetServiceClient("$home/services/SiteModel"),
		lastTap:   time.Now(),
	}

	pane.requestPane = &RequestPane{
		parent: pane,
	}

	pane.visibleTimeout = time.AfterFunc(0, func() {
		pane.keepAwake = false
		pane.visible = false
	})

	pane.introTimeout = time.AfterFunc(0, func() {
		pane.intro = false
	})

	pane.staleDataTimeout = time.AfterFunc(0, func() {
		pane.times, pane.prices = nil, nil
	})

	pane.updateTimer = time.AfterFunc(0, func() {
		if !pane.visible {
			return
		}

		err := pane.UpdateData(false)
		if err != nil {
			log.Errorf("Failed to get uber data: %s", err)
			pane.updateTimer.Reset(time.Second * 5)
		}
	})

	pane.keepAwakeTimeout = time.AfterFunc(0, func() {
		pane.keepAwake = false
	})

	go pane.Start()

	return pane
}

func (p *UberPane) Start() {

	if longitude == 0 || latitude == 0 {

		log.Infof("No --latitude and/or --longitude provided, using site location.")

		for {
			site := &model.Site{}
			err := p.siteModel.Call("fetch", config.MustString("siteId"), site, time.Second*5)

			if err == nil && (site.Longitude != nil || site.Latitude != nil) {
				longitude, latitude = *site.Longitude, *site.Latitude
				break
			}

			log.Infof("Failed to get site, or site has no location.")

			time.Sleep(time.Second * 2)
		}

	}

}

func (p *UberPane) UpdateData(once bool) error {
	times, err := client.GetTimes(latitude, longitude, user.UUID, "")
	if err != nil {
		return err
	}

	prices, err := client.GetPrices(latitude, longitude, latitude, longitude)
	if err != nil {
		return err
	}

	p.times = times
	p.prices = prices

	//spew.Dump("Updated data", times, prices)

	p.staleDataTimeout.Reset(staleDataTimeout)

	if !once && p.visible {
		p.updateTimer.Reset(updateInterval)
	}

	return nil
}

func (p *UberPane) Gesture(gesture *gestic.GestureMessage) {

	if p.requestPane.IsEnabled() {
		p.requestPane.Gesture(gesture)
		return
	}

	if gesture.Tap.Active() && time.Since(p.lastTap) > tapInterval {
		p.lastTap = time.Now()

		log.Infof("Tap!")
		if updateOnTap {
			go p.UpdateData(true)
		}
	}

	if gesture.DoubleTap.Active() && time.Since(p.lastDoubleTap) > tapInterval {
		p.lastDoubleTap = time.Now()

		log.Infof("Double Tap!")

		_, price := p.GetProduct(uberProduct)

		p.requestPane.StartRequest(uberProduct, price.SurgeMultiplier)
	}

}

func (p *UberPane) KeepAwake() bool {
	if p.requestPane.IsEnabled() {
		return true
	}

	// TODO: Screen timeouts... 10min on press etc...
	return true
}

func (p *UberPane) GetProduct(name string) (*uber.Time, *uber.Price) {
	var time *uber.Time
	var price *uber.Price

	for _, t := range p.times {
		if t.DisplayName == uberProduct {
			time = t
		}
	}

	for _, t := range p.prices {
		if t.DisplayName == uberProduct {
			price = t
		}
	}

	return time, price
}

func (p *UberPane) Render() (*image.RGBA, error) {

	p.visibleTimeout.Reset(visibleTimeout)

	if p.requestPane.IsEnabled() {
		return p.requestPane.Render()
	}

	if !p.visible {
		p.visible = true
		p.intro = true

		p.introTimeout.Reset(introDuration)

		go p.UpdateData(false)
	}

	if p.intro || p.times == nil {
		return imageLogo.GetNextFrame(), nil
	}

	time, price := p.GetProduct(uberProduct)
	var border util.Image

	if price == nil || time == nil {
		spew.Dump(p.prices, p.times)
		log.Fatalf("Could not find price/time for product %s", uberProduct)
	}

	if price.SurgeMultiplier > 1 {
		border = imageSurge
	} else {
		border = imageNoSurge
	}

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	/*draw.Draw(frame, frame.Bounds(), &image.Uniform{color.RGBA{
		R: 0,
		G: 0,
		B: 0,
		A: 255,
	}}, image.ZP, draw.Src)*/

	waitInMinutes := int(time.Estimate / 60)

	drawText := func(text string, col color.RGBA, top int) {
		width := O4b03b.Font.DrawString(img, 0, 8, text, color.Black)
		start := int(16 - width - 1)

		O4b03b.Font.DrawString(img, start, top, text, col)
	}

	drawText(fmt.Sprintf("%dm", waitInMinutes), color.RGBA{253, 151, 32, 255}, 2)
	drawText(fmt.Sprintf("%.1fx", price.SurgeMultiplier), color.RGBA{69, 175, 249, 255}, 9)

	draw.Draw(img, img.Bounds(), border.GetNextFrame(), image.Point{0, 0}, draw.Over)

	return img, nil
}

func (p *UberPane) IsEnabled() bool {
	return true
}

func (p *UberPane) IsDirty() bool {
	return true
}

type RequestPane struct {
	sync.Mutex
	parent          *UberPane
	active          bool
	state           string
	product         string
	surgeMultiplier float64
	finished        bool
}

func (p *RequestPane) StartRequest(product string, surgeMultiplier float64) {
	if p.active {
		panic("Asked to start new request... was already active.")
	}

	p.finished = false
	p.surgeMultiplier = surgeMultiplier
	p.product = product
	p.active = true
	p.state = "confirm_booking"
	go func() {
		time.Sleep(confirmTimeout)
		p.Lock()
		defer p.Unlock()
		if p.state == "confirm_booking" {
			p.active = false
		}
	}()
}

func (p *RequestPane) Gesture(gesture *gestic.GestureMessage) {

	if gesture.Tap.Active() && time.Since(p.parent.lastTap) > tapInterval {

		log.Infof("Request Tap!")

		p.parent.lastTap = time.Now()

		if p.finished { // Tap to close after a failed booking
			log.Infof("Closing failed request")
			p.active = false
			return
		}

		if p.state == "confirm_booking" {
			log.Infof("Booking!")
			p.Book()
		}

	}

	if gesture.DoubleTap.Active() && time.Since(p.parent.lastDoubleTap) > tapInterval {
		p.parent.lastDoubleTap = time.Now()

		log.Infof("Request Double Tap!")
	}

}

func (p *RequestPane) Book() {
	p.state = "processing"

	go func() {
		// TODO: Actually create the request!

		time.Sleep(time.Second * 5)
		p.updateState("accepted")
		time.Sleep(time.Second * 5)
		p.updateState("arriving")
		time.Sleep(time.Second * 5)
		p.updateState("in_progress")
		time.Sleep(time.Second * 5)
		p.updateState("trip_complete")
	}()
}

func (p *RequestPane) updateState(state string) {
	p.Lock()
	defer p.Unlock()
	p.state = state

	switch state {
	case "no_drivers_available":
		fallthrough
	case "driver_cancelled":
		fallthrough
	case "rider_cancelled":
		p.finished = true
	case "trip_complete":
		go func() {
			time.Sleep(time.Second * 5)
			p.active = false
		}()
	}
}

func (p *RequestPane) Render() (*image.RGBA, error) {

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))

	stateImg, ok := requestImages[p.state]

	if !ok {
		panic("Unknown uber request state: " + p.state)
	}

	drawText := func(text string, col color.RGBA, top int) {
		width := O4b03b.Font.DrawString(img, 0, 8, text, color.Black)
		start := int(16 - width - 1)

		O4b03b.Font.DrawString(img, start, top, text, col)
	}

	draw.Draw(img, img.Bounds(), stateImg.GetNextFrame(), image.Point{0, 0}, draw.Over)

	switch p.state {
	case "confirm_booking":
		var border util.Image

		if p.surgeMultiplier > 1 {

			stateImg, _ = requestImages["confirm_booking_surge"]

			drawText(fmt.Sprintf("%.1fx", p.surgeMultiplier), color.RGBA{69, 175, 249, 255}, 9)

			border = imageSurge
		} else {
			border = imageNoSurge
		}

		draw.Draw(img, img.Bounds(), border.GetNextFrame(), image.Point{0, 0}, draw.Over)
	}

	/*drawText := func(text string, col color.RGBA, top int) {
		width := O4b03b.Font.DrawString(img, 0, 8, text, color.Black)
		start := int(16 - width - 1)

		O4b03b.Font.DrawString(img, start, top, text, col)
	}

	drawText("woot", color.RGBA{69, 175, 249, 255}, 9)*/

	return img, nil
}

func (p *RequestPane) IsEnabled() bool {
	return p.active
}

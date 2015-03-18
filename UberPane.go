package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
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

var latitude = config.Float(0.0, "latitude")
var longitude = config.Float(0.0, "longitude")

var tapInterval = time.Duration(time.Second / 2)
var introDuration = time.Duration(time.Second * 2)

var visibleTimeout = time.Duration(time.Second * 2) // Time between frames rendered before we reset the ui.
var staleDataTimeout = time.Duration(time.Second * 90)

var timezone *time.Location

var imageSurge = util.LoadImage(util.ResolveImagePath("surge.gif"))
var imageNoSurge = util.LoadImage(util.ResolveImagePath("no_surge.gif"))
var imageLogo = util.LoadImage(util.ResolveImagePath("logo.png"))

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

	uberProduct string

	keepAwake        bool
	keepAwakeTimeout *time.Timer
}

func NewUberPane(conn *ninja.Connection) *UberPane {

	pane := &UberPane{
		siteModel:   conn.GetServiceClient("$home/services/SiteModel"),
		lastTap:     time.Now(),
		uberProduct: config.String("uberX", "uber.product"),
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

		err := pane.UpdateData()
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

func (p *UberPane) UpdateData() error {
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

	spew.Dump("Updated data", times, prices)

	p.staleDataTimeout.Reset(staleDataTimeout)

	if p.visible {
		p.updateTimer.Reset(time.Second * 30)
	}

	return nil
}

func (p *UberPane) Gesture(gesture *gestic.GestureMessage) {

	if gesture.Tap.Active() && time.Since(p.lastTap) > tapInterval {
		p.lastTap = time.Now()

		log.Infof("Tap!")
	}

	if gesture.DoubleTap.Active() && time.Since(p.lastDoubleTap) > tapInterval {
		p.lastDoubleTap = time.Now()

		log.Infof("Double Tap!")
	}

}

func (p *UberPane) KeepAwake() bool {
	return true
}

func (p *UberPane) Render() (*image.RGBA, error) {

	if !p.visible {
		p.visible = true
		p.intro = true

		p.introTimeout.Reset(introDuration)

		go p.UpdateData()
	}

	p.visibleTimeout.Reset(visibleTimeout)

	if p.intro || p.times == nil {
		return imageLogo.GetNextFrame(), nil
	}

	var time *uber.Time
	var price *uber.Price
	var border util.Image

	for _, t := range p.times {
		if t.DisplayName == p.uberProduct {
			time = t
		}
	}

	for _, t := range p.prices {
		if t.DisplayName == p.uberProduct {
			price = t
		}
	}

	if price == nil || time == nil {
		spew.Dump(p.prices, p.times)
		log.Fatalf("Could not find price/time for product %s", p.uberProduct)
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
		start := int(16 - width - 2)

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

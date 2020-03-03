package simulator

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/brocaar/chirpstack-api/go/v3/common"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
)

type deviceState int

const (
	deviceStateOTAA deviceState = iota
	deviceStateActivated
)

// Device contains the state of a simulated LoRaWAN OTAA device (1.0.x).
type Device struct {
	sync.RWMutex

	// Context to cancel device.
	ctx context.Context

	// Waitgroup to wait until simulation has been fully cancelled.
	wg *sync.WaitGroup

	// DevEUI.
	devEUI lorawan.EUI64

	// JoinEUI.
	joinEUI lorawan.EUI64

	// AppKey.
	appKey lorawan.AES128Key

	// Interval in which device sends uplinks.
	uplinkInterval time.Duration

	// Payload (plaintext) which the device sends as uplink.
	payload []byte

	// FPort used for sending uplinks.
	fPort uint8

	// Assigned device address.
	devAddr lorawan.DevAddr

	// DevNonce.
	devNonce lorawan.DevNonce

	// Uplink frame-counter.
	fCntUp uint32

	// Downlink frame-counter.
	fCntDown uint32

	// Application session-key.
	appSKey lorawan.AES128Key

	// Network session-key.
	nwkSKey lorawan.AES128Key

	// Activation state.
	state deviceState

	// Downlink frames channel (used by the gateway). Note that the gateway
	// forwards downlink frames to all associated devices, as only the device
	// is able to validate the addressee.
	downlinkFrames chan gw.DownlinkFrame

	// The associated gateway through which the device simulates its uplinks.
	gateways []*Gateway
}

// WithAppKey sets the AppKey.
func WithAppKey(appKey lorawan.AES128Key) func(*Device) {
	return func(d *Device) {
		d.appKey = appKey
	}
}

// WithDevEUI sets the DevEUI.
func WithDevEUI(devEUI lorawan.EUI64) func(*Device) {
	return func(d *Device) {
		d.devEUI = devEUI
	}
}

// WithUplinkInterval sets the uplink interval.
func WithUplinkInterval(interval time.Duration) func(*Device) {
	return func(d *Device) {
		d.uplinkInterval = interval
	}
}

// WithUplinkPayload sets the uplink payload.
func WithUplinkPayload(fPort uint8, pl []byte) func(*Device) {
	return func(d *Device) {
		d.fPort = fPort
		d.payload = pl
	}
}

// WithGateways adds the device to the given gateways.
// Use this function after WithDevEUI!
func WithGateways(gws []*Gateway) func(*Device) {
	return func(d *Device) {
		d.gateways = gws

		for i := range d.gateways {
			d.gateways[i].addDevice(d.devEUI, d.downlinkFrames)
		}
	}
}

// NewDevice creates a new device simulation.
func NewDevice(ctx context.Context, wg *sync.WaitGroup, opts ...func(*Device)) (*Device, error) {
	d := &Device{
		ctx: ctx,
		wg:  wg,

		downlinkFrames: make(chan gw.DownlinkFrame, 100),
		state:          deviceStateOTAA,
	}

	for _, o := range opts {
		o(d)
	}

	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
	}).Info("simulator: new otaa device")

	wg.Add(2)

	go d.uplinkLoop()
	go d.downlinkLoop()

	return d, nil
}

// uplinkLoop first handle the OTAA activation, after which it will periodically
// sends an uplink with the configured payload and fport.
func (d *Device) uplinkLoop() {
	defer d.wg.Done()

	var cancelled bool
	go func() {
		<-d.ctx.Done()
		cancelled = true
	}()

	time.Sleep(time.Duration(rand.Intn(int(d.uplinkInterval))))

	for !cancelled {
		switch d.getState() {
		case deviceStateOTAA:
			d.joinRequest()
			time.Sleep(6 * time.Second)
		case deviceStateActivated:
			d.unconfirmedDataUp()
			time.Sleep(d.uplinkInterval)
		}
	}
}

// downlinkLoop handles the downlink messages.
// Note: as a gateway does not know the addressee of the downlink, it is up to
// the handling functions to validate the MIC etc..
func (d *Device) downlinkLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return

		case pl := <-d.downlinkFrames:
			err := func() error {
				var phy lorawan.PHYPayload

				if err := phy.UnmarshalBinary(pl.PhyPayload); err != nil {
					return errors.Wrap(err, "unmarshal phypayload error")
				}

				switch phy.MHDR.MType {
				case lorawan.JoinAccept:
					return d.joinAccept(phy)
				}

				return nil
			}()

			if err != nil {
				log.WithError(err).Error("simulator: handle downlink frame error")
			}
		}
	}
}

// joinRequest sends the join-request.
func (d *Device) joinRequest() {
	log.WithFields(log.Fields{
		"dev_eui": d.devEUI,
	}).Debug("simulator: send OTAA request")

	phy := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.JoinRequest,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.JoinRequestPayload{
			DevEUI:   d.devEUI,
			JoinEUI:  d.joinEUI,
			DevNonce: d.getDevNonce(),
		},
	}

	if err := phy.SetUplinkJoinMIC(d.appKey); err != nil {
		log.WithError(err).Error("simulator: set uplink join mic error")
		return
	}

	d.sendUplink(phy)

	deviceJoinRequestCounter().Inc()
}

// unconfirmedDataUp sends an unconfirmed data uplink.
func (d *Device) unconfirmedDataUp() {
	log.WithFields(log.Fields{
		"dev_eui":  d.devEUI,
		"dev_addr": d.devAddr,
	}).Debug("simulator: send unconfirmed data up")

	phy := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.UnconfirmedDataUp,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: d.devAddr,
				FCnt:    d.fCntUp,
				FCtrl: lorawan.FCtrl{
					ADR: false,
				},
			},
			FPort: &d.fPort,
			FRMPayload: []lorawan.Payload{
				&lorawan.DataPayload{
					Bytes: d.payload,
				},
			},
		},
	}

	if err := phy.EncryptFRMPayload(d.appSKey); err != nil {
		log.WithError(err).Error("simulator: encrypt FRMPayload error")
		return
	}

	if err := phy.SetUplinkDataMIC(lorawan.LoRaWAN1_0, 0, 0, 0, d.nwkSKey, d.nwkSKey); err != nil {
		log.WithError(err).Error("simulator: set uplink data mic error")
		return
	}

	d.fCntUp++

	d.sendUplink(phy)

	deviceUplinkCounter().Inc()
}

// joinAccept validates and handles the join-accept downlink.
func (d *Device) joinAccept(phy lorawan.PHYPayload) error {
	err := phy.DecryptJoinAcceptPayload(d.appKey)
	if err != nil {
		return errors.Wrap(err, "decrypt join-accept payload error")
	}

	ok, err := phy.ValidateDownlinkJoinMIC(lorawan.JoinRequestType, d.joinEUI, d.devNonce, d.appKey)
	if err != nil {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid join-accept MIC")
		return nil
	}
	if !ok {
		log.WithFields(log.Fields{
			"dev_eui": d.devEUI,
		}).Debug("simulator: invalid join-accept MIC")
		return nil
	}

	jaPL, ok := phy.MACPayload.(*lorawan.JoinAcceptPayload)
	if !ok {
		return errors.New("expected *lorawan.JoinAcceptPayload")
	}

	d.appSKey, err = getAppSKey(jaPL.DLSettings.OptNeg, d.appKey, jaPL.HomeNetID, d.joinEUI, jaPL.JoinNonce, d.devNonce)
	if err != nil {
		return errors.Wrap(err, "get AppSKey error")
	}

	d.nwkSKey, err = getFNwkSIntKey(jaPL.DLSettings.OptNeg, d.appKey, jaPL.HomeNetID, d.joinEUI, jaPL.JoinNonce, d.devNonce)
	if err != nil {
		return errors.Wrap(err, "get NwkSKey error")
	}

	d.devAddr = jaPL.DevAddr

	log.WithFields(log.Fields{
		"dev_eui":  d.devEUI,
		"dev_addr": d.devAddr,
	}).Info("simulator: device OTAA activated")

	d.setState(deviceStateActivated)
	deviceJoinAcceptCounter().Inc()

	return nil
}

// sendUplink sends
func (d *Device) sendUplink(phy lorawan.PHYPayload) error {
	b, err := phy.MarshalBinary()
	if err != nil {
		return errors.Wrap(err, "marshal phypayload error")
	}

	txInfo := gw.UplinkTXInfo{
		Frequency:  868100000,
		Modulation: common.Modulation_LORA,
		ModulationInfo: &gw.UplinkTXInfo_LoraModulationInfo{
			LoraModulationInfo: &gw.LoRaModulationInfo{
				Bandwidth:       125,
				SpreadingFactor: 7,
				CodeRate:        "3/4",
			},
		},
	}

	pl := gw.UplinkFrame{
		PhyPayload: b,
		TxInfo:     &txInfo,
	}

	for i := range d.gateways {
		if err := d.gateways[i].SendUplinkFrame(pl); err != nil {
			log.WithError(err).WithFields(log.Fields{
				"dev_eui": d.devEUI,
			}).Error("simulator: send uplink frame error")
		}
	}

	return nil
}

// getDevNonce increments and returns a LoRaWAN DevNonce.
func (d *Device) getDevNonce() lorawan.DevNonce {
	d.devNonce++
	return d.devNonce
}

// getState returns the current device state.
func (d *Device) getState() deviceState {
	d.RLock()
	defer d.RUnlock()

	return d.state
}

// setState sets the device to the given state.
func (d *Device) setState(s deviceState) {
	d.Lock()
	d.Unlock()

	d.state = s
}

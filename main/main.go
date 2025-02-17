package main

import (
	"sync"

	"github.com/brocaar/chirpstack-simulator/simulator"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

func main() {
	gatewayID := lorawan.EUI64{1, 1, 1, 1, 1, 1, 1, 1}
	//devEUI := lorawan.EUI64{1, 1, 1, 1, 1, 1, 1, 1}
	//appKey := lorawan.AES128Key{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}

	var wg sync.WaitGroup
	//ctx := context.Background()

	sgw, err := simulator.NewGateway(
		simulator.WithMQTTCredentials("localhost:1883", "", ""),
		simulator.WithGatewayID(gatewayID),
		simulator.WithEventTopicTemplate("eu868/gateway/{{ .GatewayID }}/event/{{ .Event }}"),
		simulator.WithCommandTopicTemplate("eu868/gateway/{{ .GatewayID }}/command/{{ .Command }}"),
	)
	if err != nil {
		panic(err)
	}

	uplink := gw.UplinkFrame{
		PhyPayload: []byte{128, 68, 59, 16, 224, 160, 2, 0, 0, 88, 217, 127, 111, 248, 199, 200},
		TxInfo: &gw.UplinkTxInfo{
			Frequency: 868100000,
			Modulation: &gw.Modulation{
				Parameters: &gw.Modulation_Lora{
					Lora: &gw.LoraModulationInfo{
						Bandwidth:       125000,
						SpreadingFactor: 7,
						CodeRate:        gw.CodeRate_CR_4_6,
					},
				},
			},
		},
	}
	sgw.SendUplinkFrame(uplink)

	/*
		_, err = simulator.NewDevice(ctx, &wg,
			simulator.WithDevEUI(devEUI),
			simulator.WithAppKey(appKey),
			simulator.WithRandomDevNonce(),
			simulator.WithUplinkInterval(time.Second),
			simulator.WithUplinkCount(5),
			simulator.WithUplinkPayload(true, 10, []byte{1, 2, 3}),
			simulator.WithUplinkTXInfo(gw.UplinkTxInfo{
				Frequency: 868100000,
				Modulation: &gw.Modulation{
					Parameters: &gw.Modulation_Lora{
						Lora: &gw.LoraModulationInfo{
							Bandwidth:       125000,
							SpreadingFactor: 7,
							CodeRate:        gw.CodeRate_CR_4_6,
						},
					},
				},
			}),
			simulator.WithGateways([]*simulator.Gateway{sgw}),
			simulator.WithDownlinkHandlerFunc(func(conf, ack bool, fCntDown uint32, fPort uint8, data []byte) error {
				log.WithFields(log.Fields{
					"ack":       ack,
					"fcnt_down": fCntDown,
					"f_port":    fPort,
					"data":      hex.EncodeToString(data),
				}).Info("WithDownlinkHandlerFunc triggered")

				return nil
			}),
		)
	*/
	if err != nil {
		panic(err)
	}

	wg.Wait()
}

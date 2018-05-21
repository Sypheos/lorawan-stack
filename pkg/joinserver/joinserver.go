// Copyright © 2018 The Things Network Foundation, The Things Industries B.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package joinserver provides a LoRaWAN 1.1-compliant Join Server implementation.
package joinserver

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"go.thethings.network/lorawan-stack/pkg/component"
	"go.thethings.network/lorawan-stack/pkg/crypto"
	"go.thethings.network/lorawan-stack/pkg/deviceregistry"
	"go.thethings.network/lorawan-stack/pkg/errors"
	"go.thethings.network/lorawan-stack/pkg/errors/common"
	"go.thethings.network/lorawan-stack/pkg/log"
	"go.thethings.network/lorawan-stack/pkg/rpcmetadata"
	"go.thethings.network/lorawan-stack/pkg/ttnpb"
	"go.thethings.network/lorawan-stack/pkg/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var supportedMACVersions = [...]ttnpb.MACVersion{
	ttnpb.MAC_V1_0,
	ttnpb.MAC_V1_0_1,
	ttnpb.MAC_V1_0_2,
	ttnpb.MAC_V1_1,
}

// JoinServer implements the Join Server component.
//
// The Join Server exposes the NsJs and DeviceRegistry services.
type JoinServer struct {
	*component.Component
	*deviceregistry.RegistryRPC

	registry    deviceregistry.Interface
	euiPrefixes []types.EUI64Prefix
}

// Config represents the JoinServer configuration.
type Config struct {
	Registry        deviceregistry.Interface `name:"-"`
	JoinEUIPrefixes []types.EUI64Prefix      `name:"join-eui-prefix" description:"JoinEUI prefixes handled by this JS"`
}

// New returns new *JoinServer.
func New(c *component.Component, conf *Config, rpcOptions ...deviceregistry.RPCOption) (*JoinServer, error) {
	rpcOptions = append(rpcOptions, deviceregistry.ForComponents(ttnpb.PeerInfo_JOIN_SERVER))
	registryRPC, err := deviceregistry.NewRPC(c, conf.Registry, rpcOptions...)
	if err != nil {
		return nil, err
	}

	js := &JoinServer{
		Component:   c,
		RegistryRPC: registryRPC,
		registry:    conf.Registry,
		euiPrefixes: conf.JoinEUIPrefixes,
	}
	c.RegisterGRPC(js)
	return js, nil
}

func keyPointer(key types.AES128Key) *types.AES128Key {
	return &key
}

func checkMIC(key types.AES128Key, rawPayload []byte) error {
	if n := len(rawPayload); n != 23 {
		return errors.Errorf("Expected length of raw payload to be equal to 23, got %d", n)
	}
	computed, err := crypto.ComputeJoinRequestMIC(key, rawPayload[:19])
	if err != nil {
		return ErrMICComputeFailed.New(nil)
	}
	for i := 0; i < 4; i++ {
		if computed[i] != rawPayload[19+i] {
			return ErrMICMismatch.New(nil)
		}
	}
	return nil
}

// HandleJoin is called by the Network Server to join a device
func (js *JoinServer) HandleJoin(ctx context.Context, req *ttnpb.JoinRequest) (resp *ttnpb.JoinResponse, err error) {
	logger := log.FromContext(ctx)

	ver := req.GetSelectedMacVersion()

	supported := false
	for _, v := range supportedMACVersions {
		if v == ver {
			supported = true
			break
		}
	}
	if !supported {
		return nil, common.ErrUnsupportedLoRaWANMACVersion.New(errors.Attributes{
			"version": ver,
		})
	}

	if req.EndDeviceIdentifiers.DevAddr == nil {
		return nil, common.ErrMissingDevAddr.New(nil)
	}
	devAddr := *req.EndDeviceIdentifiers.DevAddr

	rawPayload := req.GetRawPayload()
	if req.Payload.GetPayload() == nil {
		if rawPayload == nil {
			return nil, common.ErrMissingPayload.New(nil)
		}
		if err = req.Payload.UnmarshalLoRaWAN(rawPayload); err != nil {
			return nil, common.ErrUnmarshalPayloadFailed.NewWithCause(nil, err)
		}
	}

	msg := req.GetPayload()
	if msg.GetMajor() != ttnpb.Major_LORAWAN_R1 {
		return nil, common.ErrUnsupportedLoRaWANVersion.New(errors.Attributes{
			"version": msg.GetMajor(),
		})
	}
	if msg.GetMType() != ttnpb.MType_JOIN_REQUEST {
		return nil, ErrWrongPayloadType.New(errors.Attributes{
			"type": req.Payload.MType,
		})
	}

	pld := msg.GetJoinRequestPayload()
	if pld == nil {
		return nil, ErrMissingJoinRequest.New(nil)
	}

	if pld.DevEUI.IsZero() {
		return nil, common.ErrMissingDevEUI.New(nil)
	}
	if pld.JoinEUI.IsZero() {
		return nil, common.ErrMissingJoinEUI.New(nil)
	}

	if rawPayload == nil {
		rawPayload, err = req.Payload.MarshalLoRaWAN()
		if err != nil {
			panic(errors.NewWithCause(err, "Failed to marshal join request payload"))
		}
	}

	dev, err := deviceregistry.FindByIdentifiers(js.registry, &ttnpb.EndDeviceIdentifiers{
		DevEUI:  &pld.DevEUI,
		JoinEUI: &pld.JoinEUI,
	})
	if err != nil {
		return nil, err
	}

	if rpcmetadata.FromIncomingContext(ctx).NetAddress != dev.GetNetworkServerAddress() {
		return nil, ErrAddressMismatch.New(errors.Attributes{
			"component": "Network Server",
		})
	}

	match := false
	for _, p := range js.euiPrefixes {
		if p.Matches(pld.JoinEUI) {
			match = true
			break
		}
	}
	switch {
	case !match && dev.GetLoRaWANVersion() == ttnpb.MAC_V1_0:
		return nil, ErrUnknownAppEUI.New(nil)
	case !match:
		// TODO determine the cluster containing the device
		// https://github.com/TheThingsIndustries/ttn/issues/244
		return nil, ErrForwardJoinRequest.NewWithCause(nil, deviceregistry.ErrDeviceNotFound.New(nil))
	}

	// Registered version is lower than selected.
	if dev.LoRaWANVersion.Compare(ver) == -1 {
		return nil, ErrMACVersionMismatch.New(errors.Attributes{
			"registered": dev.GetLoRaWANVersion(),
			"selected":   ver,
		})
	}

	ke := dev.GetRootKeys().GetAppKey()
	if ke == nil {
		return nil, common.ErrCorruptRegistry.NewWithCause(nil, ErrAppKeyEnvelopeNotFound.New(nil))
	}
	if ke.Key == nil || ke.Key.IsZero() {
		return nil, common.ErrCorruptRegistry.NewWithCause(nil, ErrAppKeyNotFound.New(nil))
	}
	appKey := *ke.Key

	var b []byte
	if req.GetCFList() == nil {
		b = make([]byte, 0, 17)
	} else {
		b = make([]byte, 0, 33)
	}

	b, err = (&ttnpb.MHDR{
		MType: ttnpb.MType_JOIN_ACCEPT,
		Major: msg.GetMajor(),
	}).AppendLoRaWAN(b)
	if err != nil {
		panic(errors.NewWithCause(err, "Failed to encode join accept MHDR"))
	}

	var jn types.JoinNonce
	nb := make([]byte, 4)
	binary.LittleEndian.PutUint32(nb, dev.NextJoinNonce)
	copy(jn[:], nb)

	b, err = (&ttnpb.JoinAcceptPayload{
		NetID:      req.NetID,
		JoinNonce:  jn,
		CFList:     req.GetCFList(),
		DevAddr:    devAddr,
		DLSettings: req.GetDownlinkSettings(),
		RxDelay:    req.GetRxDelay(),
	}).AppendLoRaWAN(b)
	if err != nil {
		panic(errors.NewWithCause(err, "Failed to encode join accept MAC payload"))
	}

	dn := binary.LittleEndian.Uint16(pld.DevNonce[:])
	if !dev.GetDisableJoinNonceCheck() {
		switch ver {
		case ttnpb.MAC_V1_1:
			if uint32(dn) < dev.NextDevNonce {
				return nil, ErrDevNonceTooSmall.New(nil)
			}
			if dev.NextDevNonce == math.MaxUint32 {
				return nil, ErrDevNonceTooHigh.New(nil)
			}
			dev.NextDevNonce = uint32(dn + 1)
		case ttnpb.MAC_V1_0, ttnpb.MAC_V1_0_1, ttnpb.MAC_V1_0_2:
			for _, used := range dev.UsedDevNonces {
				if dn == uint16(used) {
					return nil, ErrDevNonceReused.New(nil)
				}
			}
		default:
			panic("This statement is unreachable. Fix version check.")
		}
	}

	switch ver {
	case ttnpb.MAC_V1_1:
		ke := dev.GetRootKeys().GetNwkKey()
		if ke == nil {
			return nil, common.ErrCorruptRegistry.NewWithCause(nil, ErrNwkKeyEnvelopeNotFound.New(nil))
		}
		if ke.Key == nil || ke.Key.IsZero() {
			return nil, common.ErrCorruptRegistry.NewWithCause(nil, ErrNwkKeyNotFound.New(nil))
		}
		nwkKey := *ke.Key

		if err := checkMIC(nwkKey, rawPayload); err != nil {
			return nil, ErrMICCheckFailed.NewWithCause(nil, err)
		}

		mic, err := crypto.ComputeJoinAcceptMIC(crypto.DeriveJSIntKey(nwkKey, pld.DevEUI), 0xff, pld.JoinEUI, pld.DevNonce, b)
		if err != nil {
			return nil, common.ErrComputeMIC.NewWithCause(nil, err)
		}

		enc, err := crypto.EncryptJoinAccept(nwkKey, append(b[1:], mic[:]...))
		if err != nil {
			return nil, ErrEncryptPayloadFailed.NewWithCause(nil, err)
		}
		resp = &ttnpb.JoinResponse{
			RawPayload: append(b[:1], enc...),
			SessionKeys: ttnpb.SessionKeys{
				FNwkSIntKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveFNwkSIntKey(nwkKey, jn, pld.JoinEUI, pld.DevNonce)),
					KEKLabel: "",
				},
				SNwkSIntKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveSNwkSIntKey(nwkKey, jn, pld.JoinEUI, pld.DevNonce)),
					KEKLabel: "",
				},
				NwkSEncKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveNwkSEncKey(nwkKey, jn, pld.JoinEUI, pld.DevNonce)),
					KEKLabel: "",
				},
				// TODO: Encrypt key with AS KEK https://github.com/TheThingsIndustries/ttn/issues/271
				AppSKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveAppSKey(appKey, jn, pld.JoinEUI, pld.DevNonce)),
					KEKLabel: "",
				},
			},
			Lifetime: nil,
		}
	case ttnpb.MAC_V1_0, ttnpb.MAC_V1_0_1, ttnpb.MAC_V1_0_2:
		if err := checkMIC(appKey, rawPayload); err != nil {
			return nil, ErrMICCheckFailed.NewWithCause(nil, err)
		}

		mic, err := crypto.ComputeLegacyJoinAcceptMIC(appKey, b)
		if err != nil {
			return nil, common.ErrComputeMIC.NewWithCause(nil, err)
		}

		enc, err := crypto.EncryptJoinAccept(appKey, append(b[1:], mic[:]...))
		if err != nil {
			return nil, ErrEncryptPayloadFailed.NewWithCause(nil, err)
		}
		resp = &ttnpb.JoinResponse{
			RawPayload: append(b[:1], enc...),
			SessionKeys: ttnpb.SessionKeys{
				FNwkSIntKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveLegacyNwkSKey(appKey, jn, req.NetID, pld.DevNonce)),
					KEKLabel: "",
				},
				AppSKey: &ttnpb.KeyEnvelope{
					Key:      keyPointer(crypto.DeriveLegacyAppSKey(appKey, jn, req.NetID, pld.DevNonce)),
					KEKLabel: "",
				},
			},
			Lifetime: nil,
		}
	default:
		panic("This statement is unreachable. Fix version check.")
	}

	dev.UsedDevNonces = append(dev.UsedDevNonces, uint32(dn))
	dev.NextJoinNonce++
	dev.EndDevice.Session = &ttnpb.Session{
		StartedAt:   time.Now().UTC(),
		DevAddr:     devAddr,
		SessionKeys: resp.SessionKeys,
	}
	if err := dev.Store(); err != nil {
		logger.WithFields(log.Fields(
			"dev_eui", dev.EndDeviceIdentifiers.DevEUI,
			"join_eui", dev.EndDeviceIdentifiers.JoinEUI,
			"application_id", dev.EndDeviceIdentifiers.GetApplicationID(),
			"device_id", dev.EndDeviceIdentifiers.GetDeviceID(),
		)).WithError(err).Error("Failed to update device")
	}
	return resp, nil
}

// GetAppSKey returns the AppSKey associated with device specified by the supplied request.
func (js *JoinServer) GetAppSKey(ctx context.Context, req *ttnpb.SessionKeyRequest) (*ttnpb.AppSKeyResponse, error) {
	if req.DevEUI.IsZero() {
		return nil, common.ErrMissingDevEUI.New(nil)
	}
	if req.GetSessionKeyID() == "" {
		return nil, ErrMissingSessionKeyID.New(nil)
	}

	dev, err := deviceregistry.FindByIdentifiers(js.registry, &ttnpb.EndDeviceIdentifiers{
		DevEUI: &req.DevEUI,
	})
	if err != nil {
		return nil, err
	}

	if rpcmetadata.FromIncomingContext(ctx).NetAddress != dev.ApplicationServerAddress {
		return nil, ErrAddressMismatch.New(errors.Attributes{
			"component": "Application Server",
		})
	}

	s := dev.GetSession()
	if s == nil {
		return nil, ErrNoSession.New(nil)
	}
	if s.GetSessionKeyID() != req.GetSessionKeyID() {
		s = dev.GetSessionFallback()
		if s == nil || s.GetSessionKeyID() != req.GetSessionKeyID() {
			return nil, ErrSessionKeyIDMismatch.New(nil)
		}
	}

	appSKey := s.GetAppSKey()
	if appSKey == nil {
		return nil, ErrAppSKeyEnvelopeNotFound.New(nil)
	}
	// TODO: Encrypt key with AS KEK https://github.com/TheThingsIndustries/ttn/issues/271
	return &ttnpb.AppSKeyResponse{
		AppSKey: *appSKey,
	}, nil
}

// GetNwkSKeys returns the NwkSKeys associated with device specified by the supplied request.
func (js *JoinServer) GetNwkSKeys(ctx context.Context, req *ttnpb.SessionKeyRequest) (*ttnpb.NwkSKeysResponse, error) {
	if req.DevEUI.IsZero() {
		return nil, common.ErrMissingDevEUI.New(nil)
	}
	if req.GetSessionKeyID() == "" {
		return nil, ErrMissingSessionKeyID.New(nil)
	}

	dev, err := deviceregistry.FindByIdentifiers(js.registry, &ttnpb.EndDeviceIdentifiers{
		DevEUI: &req.DevEUI,
	})
	if err != nil {
		return nil, err
	}

	if rpcmetadata.FromIncomingContext(ctx).NetAddress != dev.NetworkServerAddress {
		return nil, ErrAddressMismatch.New(errors.Attributes{
			"component": "Network Server",
		})
	}

	s := dev.GetSession()
	if s == nil {
		return nil, ErrNoSession.New(nil)
	}
	if s.GetSessionKeyID() != req.GetSessionKeyID() {
		s = dev.GetSessionFallback()
		if s == nil || s.GetSessionKeyID() != req.GetSessionKeyID() {
			return nil, ErrSessionKeyIDMismatch.New(nil)
		}
	}

	nwkSEncKey := s.GetNwkSEncKey()
	if nwkSEncKey == nil {
		return nil, ErrNwkSEncKeyEnvelopeNotFound.New(nil)
	}
	fNwkSIntKey := s.GetFNwkSIntKey()
	if fNwkSIntKey == nil {
		return nil, ErrFNwkSIntKeyEnvelopeNotFound.New(nil)
	}
	sNwkSIntKey := s.GetSNwkSIntKey()
	if sNwkSIntKey == nil {
		return nil, ErrSNwkSIntKeyEnvelopeNotFound.New(nil)
	}
	// TODO: Encrypt key with AS KEK https://github.com/TheThingsIndustries/ttn/issues/271
	return &ttnpb.NwkSKeysResponse{
		NwkSEncKey:  *nwkSEncKey,
		FNwkSIntKey: *fNwkSIntKey,
		SNwkSIntKey: *sNwkSIntKey,
	}, nil
}

// Roles of the gRPC service
func (js *JoinServer) Roles() []ttnpb.PeerInfo_Role {
	return []ttnpb.PeerInfo_Role{ttnpb.PeerInfo_JOIN_SERVER}
}

// RegisterServices registers services provided by js at s.
func (js *JoinServer) RegisterServices(s *grpc.Server) {
	ttnpb.RegisterNsJsServer(s, js)
	ttnpb.RegisterJsDeviceRegistryServer(s, js)
}

// RegisterHandlers registers gRPC handlers.
func (js *JoinServer) RegisterHandlers(s *runtime.ServeMux, conn *grpc.ClientConn) {
	ttnpb.RegisterJsDeviceRegistryHandler(js.Context(), s, conn)
}

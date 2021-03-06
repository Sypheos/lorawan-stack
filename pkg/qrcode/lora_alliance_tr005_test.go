// Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
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

package qrcode_test

import (
	"testing"

	"github.com/smartystreets/assertions"
	"go.thethings.network/lorawan-stack/pkg/errors"
	. "go.thethings.network/lorawan-stack/pkg/qrcode"
	"go.thethings.network/lorawan-stack/pkg/types"
	"go.thethings.network/lorawan-stack/pkg/util/test"
	"go.thethings.network/lorawan-stack/pkg/util/test/assertions/should"
)

func TestLoRaAllianceTR005Draft2(t *testing.T) {
	for _, tc := range []struct {
		Name           string
		Data           []byte
		CanonicalData  []byte
		Expected       LoRaAllianceTR005Draft2
		ErrorAssertion func(t *testing.T, err error) bool
	}{
		{
			Name: "Simple",
			Data: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42"),
			Expected: LoRaAllianceTR005Draft2{
				JoinEUI:  types.EUI64{0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				DevEUI:   types.EUI64{0x42, 0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				VendorID: [2]byte{0x42, 0xff},
				ModelID:  [2]byte{0xff, 0x42},
			},
		},
		{
			Name: "Extensions",
			Data: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42:%V0102%SSERIAL%PPROPRIETARY"),
			Expected: LoRaAllianceTR005Draft2{
				JoinEUI:              types.EUI64{0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				DevEUI:               types.EUI64{0x42, 0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				VendorID:             [2]byte{0x42, 0xff},
				ModelID:              [2]byte{0xff, 0x42},
				DeviceValidationCode: []byte{0x01, 0x02},
				SerialNumber:         "SERIAL",
				Proprietary:          "PROPRIETARY",
			},
		},
		{
			Name:          "EmptyExtensions",
			Data:          []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42:%V%S%P"),
			CanonicalData: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42"),
			Expected: LoRaAllianceTR005Draft2{
				JoinEUI:              types.EUI64{0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				DevEUI:               types.EUI64{0x42, 0x42, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
				VendorID:             [2]byte{0x42, 0xff},
				ModelID:              [2]byte{0xff, 0x42},
				DeviceValidationCode: []byte{},
			},
		},
		{
			Name: "Invalid/Type",
			Data: []byte{0x42, 0xff, 0x42, 0x42},
			ErrorAssertion: func(t *testing.T, err error) bool {
				return assertions.New(t).So(errors.IsInvalidArgument(err), should.BeTrue)
			},
		},
		{
			Name: "Invalid/EUI",
			Data: []byte("URN:LW:DP:42FFFFFFFF:4242FFFFFFFFFFFF:42FFFF42"),
			ErrorAssertion: func(t *testing.T, err error) bool {
				return assertions.New(t).So(errors.IsInvalidArgument(err), should.BeTrue)
			},
		},
		{
			Name: "Invalid/ProdID",
			Data: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42AABB"),
			ErrorAssertion: func(t *testing.T, err error) bool {
				return assertions.New(t).So(errors.IsInvalidArgument(err), should.BeTrue)
			},
		},
		{
			Name: "Invalid/DevVCode",
			Data: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42:%VGHIJKLMN"),
			ErrorAssertion: func(t *testing.T, err error) bool {
				return assertions.New(t).So(errors.IsInvalidArgument(err), should.BeTrue)
			},
		},
		{
			Name: "Invalid/ExtensionChars",
			Data: []byte("URN:LW:DP:42FFFFFFFFFFFFFF:4242FFFFFFFFFFFF:42FFFF42:%P#_"),
			ErrorAssertion: func(t *testing.T, err error) bool {
				return assertions.New(t).So(errors.IsInvalidArgument(err), should.BeTrue)
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			a := assertions.New(t)

			var data LoRaAllianceTR005Draft2
			err := data.UnmarshalText(tc.Data)
			if tc.ErrorAssertion != nil && a.So(tc.ErrorAssertion(t, err), should.BeTrue) {
				return
			}
			if !a.So(err, should.BeNil) || !a.So(data, should.Resemble, tc.Expected) {
				t.FailNow()
			}

			canonical := tc.CanonicalData
			if canonical == nil {
				canonical = tc.Data
			}

			text := test.Must(data.MarshalText()).([]byte)
			a.So(string(text), should.Equal, string(canonical))
		})
	}
}

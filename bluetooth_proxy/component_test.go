package bluetooth_proxy

import (
	"testing"

	"gotest.tools/v3/assert"
	"tinygo.org/x/bluetooth"
)

func TestBleAddressToUint64(t *testing.T) {
	input := bluetooth.MAC([]uint8{1, 2, 3, 4, 5, 6})
	expected := uint64(0x060504030201)
	actual := bleAddressToUint64(input)
	assert.Equal(t, expected, actual)
}

package bridge

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The host reads the snapshot by index; sim/layout.mjs must name the same ones.
func TestLayoutMatchesHost(t *testing.T) {
	src, err := os.ReadFile("../sim/layout.mjs")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"t": HT, "created": HCreated, "delivered": HDelivered, "tx": HTX, "rotations": HRotations,
		"agedOut": HAgedOut, "custody": HCustody, "graphVer": HGraphVer, "src": HSrc, "dst": HDst,
		"track": HTrack, "trackId": HTrackID, "trackTX": HTrackTX, "trackHops": HTrackHops,
		"trackBorn": HTrackBorn, "trackDone": HTrackDone, "trackExpired": HTrackExpired,
		"trackCopiesSum": HTrackCopiesSum, "trackTicks": HTrackTicks, "trackAt": HTrackAt,
		"trackDoneAt": HTrackDoneAt, "trackFloodTX": HTrackFloodTX, "trackWanted": HTrackWanted,
		"trackCopies": HTrackCopies, "sends": HSends, "sendOutcome": HSendOutcome, "sendHops": HSendHops,
		"sendTicks": HSendTicks, "sendTX": HSendTX, "sendCopiesSum": HSendCopiesSum,
		"sendCopyTicks": HSendCopyTicks, "transfers": HTransfers, "flash": HFlash, "overlay": HOverlay,
		"leaders": HLeaders, "round": HRound, "nodes": HNodes, "words": HeaderWords,
	}
	re := regexp.MustCompile(`(\w+): (\d+)`)
	got := map[string]int{}
	header := regexp.MustCompile(`(?s)export const HEADER = \{(.*?)\};`).FindSubmatch(src)
	if header == nil {
		t.Fatal("HEADER not found in layout.mjs")
	}
	for _, m := range re.FindAllSubmatch(header[1], -1) {
		n, _ := strconv.Atoi(string(m[2]))
		got[string(m[1])] = n
	}
	if len(got) != len(want) {
		t.Fatalf("layout.mjs names %d fields, bridge defines %d", len(got), len(want))
	}
	for name, idx := range want {
		if got[name] != idx {
			t.Errorf("%s: layout.mjs says %d, bridge says %d", name, got[name], idx)
		}
	}
	for name, idx := range map[string]int{"TRANSFER_WORDS": TransferWords, "FLASH_WORDS": FlashWords} {
		m := regexp.MustCompile(`export const ` + name + ` = (\d+);`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("%s missing from layout.mjs", name)
		}
		if n, _ := strconv.Atoi(string(m[1])); n != idx {
			t.Errorf("%s: layout.mjs says %d, bridge says %d", name, n, idx)
		}
	}
}

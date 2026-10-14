// Snapshot layout shared with bridge/bridge.go (bridge/layout_test.go checks
// that the two agree).
export const HEADER = {
  t: 0, created: 1, delivered: 2, tx: 3, rotations: 4, agedOut: 5, custody: 6, graphVer: 7, src: 8, dst: 9,
  track: 10, trackId: 11, trackTX: 12, trackHops: 13, trackBorn: 14, trackDone: 15, trackExpired: 16,
  trackCopiesSum: 17, trackTicks: 18, trackAt: 19, trackDoneAt: 20, trackFloodTX: 21, trackWanted: 22,
  trackCopies: 23, sends: 24, sendOutcome: 25, sendHops: 26, sendTicks: 27, sendTX: 28,
  sendCopiesSum: 29, sendCopyTicks: 30, transfers: 31, flash: 32, overlay: 33, leaders: 34, round: 35, nodes: 36, words: 37,
};
export const TRANSFER_WORDS = 7;
export const FLASH_WORDS = 4;
export const INFO = { nodesPerCell: 0, total: 1, capped: 2, meanRange: 3, meanLink: 4, boundaryShare: 5 };

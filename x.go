package x

// An EpochHash holds the hash for the current epoch. It is generated by an
// Ethereum smart contract every time the a new epoch begins.
type EpochHash struct {
	Hash []byte
}

// A MinerHash holds the hash for a miner. It is generated by an Ethereum smart
// contract during the registration of the miner.
type MinerHash struct {
	Hash []byte
}

type MNetwork struct {
}

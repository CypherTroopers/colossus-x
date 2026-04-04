package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	cx "colossusx/colossusx"
	"golang.org/x/crypto/sha3"
)

type Hash [32]byte

type EconomicConfig struct {
	BlockReward            uint64    `json:"block_reward,omitempty"
	TargetBlockTimeMillis uint64    `json:"target_block_time_millis,omitempty"`
SRetargetInterval       uint64    `json:"retarget_interval,omitempty"`
	MaxTarget             cx.Target `json:"max_target,omitempty"`
}

func (c EconomicConfig) Normalized() EconomicConfig {
	if c.BlockReward == 0 {
		c.BlockReward = 50
	}
	if c.TargetBlockTimeMillis == 0 {
		c.TargetBlockTimeMillis = 18_000
	}
	if c.RetargetInterval == 0 {
		c.RetargetInterval = 20
	}
	return c
}

func (c EconomicConfig) TargetBlockTime() time.Duration {
	cfg := c.Normalized()
	return time.Duration(cfg.TargetBlockTimeMillis) * time.Millisecond
}

type Transaction struct {
	From  string `json:"from"`
To    string `json:"to"`
	Value uint64 `son:"value"`
	Nonce uint64 `json:"nonce"`
	Data  string `son:"data,omitempty"`
}

type AccountState struct {
	Balance uint64 `son:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type BlockHeader struct {
	Version          uint32    `json:"version"`
	AlgorithmVersion uint32    `json:"algorithm_version"`
	Height          uint64    `json:"height"`
ParentHash       Hash      `json:"parent_hash"`
Timestamp        int64     `json:"timestamp"`
	Target          cx.Target `json:"target"`
	Nonce          uint64    `json:"nonce"`
	Coinbase        string    `json:"coinbase,omitempty"`
	EpochSeed       Hash       `json:"epoch_seed"`
	DAGSizeBytes     uint64    `json:"dag_size_bytes"`
DAGMerkleRoot    Hash      `json:"dag_merkle_root"`
	TxRoot          Hash      `json:"tx_root"`
StateRoot        Hash      `json:"state_root"`
}

type Block struct {
	Header                   BlockHeader                 `json:"header"`
	Transactions             []Transaction                `json:"transactions,omitempty"`	State                    map[string]AccountState      `json:"state,omitempty"`
ColossusXSolution        *cx.ColossusXSolution         `json:"colossusx_solution,omitempty"`
ColossusXSolutionCompact *cx.ColossusXSolutionCompact `json:"colossusx_solution_compact,omitempty"`
}

type GenesisConfig struct {
	ChainID   string            `json:"chain_id"`
	Message   string            `json:"message,omitempty"`
Timestamp  int64            `json:"timestamp"`
	Bits      cx.Target       `json:"target"`
Spec      cx.Spec         `json:"spec"`
	ExtraData string            `json:"extra_data,omitempty"`
Alloc     map[string]uint64 `json:"alloc,omitempty"`
	Economics EconomicConfig    `json:"economics,omitempty"`
}

type ChainConfig struct {
	NetworkID string         `json:"network_id"}
	Spec      cx.Spec       `œÛÛˆœÜXÈ˜‚QXÛÛ›ÛZXÜÈXÛÛ›ÛZXĞÛÛ™šYÈÛÛˆ™XÛÛ›ÛZXÜËÛZ][\H˜ŸB‚\HY\”İ]\ÈİXİÂ‚TY\’Qİš[™ÈœÛÛˆœY\—ÚYŸB‚P™\İ\Ú\ÚœÛÛˆ˜™\İÚ\Ú˜‚P™\İZYÚZ[ÛÛˆ˜™\İÚZYÚ˜‚Uİ[ÛÜšÈİš[™ÈœÛÛˆİ[İÛÜšÈŸB‚PÛÛ›™XİY][œÛÛˆ˜ÛÛ›™XİYØ]ŸBŸB‚\HZ[š[™Õ[\]HİXİÂ‚T\™[\ÚœÛÛˆœ\™[ŸB‚RZYÚZ[œÛÛˆšZYÚ˜‚U\™Ù]Ş•\™Ù]œÛÛˆ\™Ù]˜‚Q\ØÚÙYY\ÚœÛÛˆ™\ØÚÜÙYY˜‚PÜ™X]Y][YK•[YHœÛÛˆ˜Ü™X]YØ]˜‚RXY\ˆ›ØÚÒXY\ˆœÛÛˆšXY\ˆ˜ŸB‚™[˜È
\Ú
Hİš[™Ê
Hİš[™ÈÈ™]\›ˆ^‘[˜ÛÙUÔİš[™ÊÎ—JHB™[˜È
\Ú
HX\œÚ[”ÓÓŠ
H
×X]K\œ›ÜŠHÈ™]\›ˆœÛÛ‹“X\œÚ[
”İš[™Ê
JHB™[˜È

’\Ú
H[›X\œÚ[”ÓÓŠ]H×X]JH\œ›ÜˆÂ‚]˜\ˆÈİš[™Â‚ZYˆ\œˆHœÛÛ‹•[›X\œÚ[
]K	œÊNÈ\œˆOHš[Â‚B\™]\›ˆ\œ‚‚_B‚YXÛÙY\œˆH^‘XÛÙTİš[™ÊÊB‚ZYˆ\œˆOHš[Â‚B\™]\›ˆ\œ‚‚_B‚ZYˆ[ŠXÛÙY
HOH[Š
HÂ‚B\™]\›ˆ›]‘\œ›Ü™Š™^XİY	Y]\ËÛİ	Y‹[Š
K[ŠXÛÙY
JB‚_B‚XÛÜJÎ—KXÛÙY
B‚\™]\›ˆš[ŸB‚™[˜È
›ØÚÒXY\ŠH[˜ÛÙQ›Ü“Z[š[™Ê
H×X]HÂ‚XYˆHXZÙJ×X]K
Í
Î
ÌÌŠÎ
ÌÌŠÎ
Í
Û[ŠÛÚ[˜˜\ÙJJÌÌŠÎ
ÌÌŠÌÌŠÌÌŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[ÌŠY‹•™\œÚ[ÛŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[ÌŠY‹[ÛÜš]U™\œÚ[ÛŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹’ZYÚ
B‚XYˆH\[™
Y‹”\™[\ÚÎ—K‹‹ŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹Z[
•[Y\İ[\
JB‚XYˆH\[™
Y‹•\™Ù]Î—K‹‹ŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[ÌŠY‹Z[ÌŠ[ŠÛÚ[˜˜\ÙJJJB‚XYˆH\[™
Y‹×X]JÛÚ[˜˜\ÙJK‹‹ŠB‚XYˆH\[™
Y‹‘\ØÚÙYYÎ—K‹‹ŠB‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹‘QÔÚ^™P]\ÊB‚XYˆH\[™
Y‹‘QÓY\šÛT›ÛİÎ—K‹‹ŠB‚XYˆH\[™
Y‹•›ÛİÎ—K‹‹ŠB‚XYˆH\[™
Y‹”İ]T›ÛİÎ—K‹‹ŠB‚\™]\›ˆY‚ŸB‚™[˜È
›ØÚÒXY\ŠH[˜ÛÙJ
H×X]HÂ‚XYˆH‘[˜ÛÙQ›Ü“Z[š[™Ê
B‚XYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹“›Û˜ÙJB‚\™]\›ˆY‚ŸB‚™[˜È
›ØÚÒXY\ŠHXY\’\Ú

H\ÚÈ™]\›ˆÚLM‹”İ[LMŠ‘[˜ÛÙJ
JHB™[˜È
ˆ›ØÚÊH›ØÚÒ\Ú

H\ÚÈ™]\›ˆ‹’XY\‹’XY\’\Ú

HB‚™[˜È™]ÑÙ[™\Ú\Ğ›ØÚÊÙ™ÈÙ[™\Ú\ĞÛÛ™šYÊH›ØÚÈÂ‚\™\ÛÛ™YHÙ™Ë”ÜXË”™\ÛÛ™Y›Ü’ZYÚ

B‚\İ]HHXZÙJX\Üİš[™×PXØÛİ[İ]K[ŠÙ™Ë[ØÊJB‚Y›ÜˆY‹˜[[˜ÙHH˜[™ÙHÙ™Ë[ØÈÂ‚B\İ]VØY—HHXØÛİ[İ]^Ğ˜[[˜ÙNˆ˜[[˜Ù_B‚_B‚\™]\›ˆ›ØÚŞÂ‚BRXY\ˆ›ØÚÒXY\×—™\œÚ[ÛˆK—[ÛÜš]U™\œÚ[Ûˆ™\ÛÛ™Y[ÛÜš]U™\œÚ[Û‹—ZYÚˆ—\™[\Úˆ\ÚßK—[Y\İ[\ˆÙ™Ë•[Y\İ[\—\™Ù]ˆÙ™Ëš]Ë—›Û˜ÙNˆ—ÛÚ[˜˜\ÙNˆ‹—\ØÚÙYYˆ\ØÚÙYY›Ü’ZYÚ
™\ÛÛ™Y
K—QÔÚ^™P]\Îˆ™\ÛÛ™Y‘QÔÚ^™P]\Ë—QÓY\šÛT›Ûİˆ\ÚßK—›ÛİˆÛÛ\]U›Ûİ
š[
K—İ]T›ÛİˆÛÛ\]Tİ]T›Ûİ
İ]JK—K—˜[œØXİ[ÛœÎˆš[—İ]Nˆİ]K—WŸB‚™[˜È\ØÚÙYY›Ü’ZYÚ
ÜXÈŞ”ÜXËZYÚZ[
H\ÚÂ‚]˜\ˆÙYYX]\šX[ÍX]B‚Y\ØÚHZ[

B‚ZYˆÜXË‘\ØÚ›ØÚÜÈOHÂ‚BY\ØÚHZYÚÈÜXË‘\ØÚ›ØÚÜÂ‚_B‚Xš[˜\KšYÑ[™X[‹”]Z[
ÙYYX]\šX[ÎK\ØÚ
B‚XÛÜJÙYYX]\šX[ÎKÜXË‘Ù[™\Ú\Ò\ÚÎ—JB‚\™]\›ˆÚLË”İ[LMŠÙYYX]\šX[Î—JBŸB‚™[˜ÈÛÛ™Tİ]J[ˆX\Üİš[™×PXØÛİ[İ]JHX\Üİš[™×PXØÛİ[İ]HÂ‚ZYˆ[Š[ŠHOHÂ‚B\™]\›ˆX\Üİš[™×PXØÛİ[İ]^ßB‚_B‚[İ]HXZÙJX\Üİš[™×PXØÛİ[İ]K[Š[ŠJB‚Y›ÜˆËˆH˜[™ÙH[ˆÂ‚B[İ]Ú×HH‚‚_B‚\™]\›ˆİ]ŸB‚™[˜ÈÛÛ\]U›Ûİ
È×U˜[œØXİ[ÛŠH\ÚÂ‚ZYˆ[ŠÊHOHÂ‚B\™]\›ˆÚLM‹”İ[LMŠš[
B‚_B‚\^[ØYÈHœÛÛ‹“X\œÚ[
ÊB‚\™]\›ˆÚLM‹”İ[LMŠ^[ØY
BŸB‚™[˜ÈÛÛ\]Tİ]T›Ûİ
İ]HX\Üİš[™×PXØÛİ[İ]JH\ÚÂ‚ZYˆ[Šİ]JHOHÂ‚B\™]\›ˆÚLM‹”İ[LMŠš[
B‚_B‚ZÙ^\ÈHXZÙJ×\İš[™Ë[Šİ]JJB‚Y›ÜˆÈH˜[™ÙHİ]HÂ‚BZÙ^\ÈH\[™
Ù^\ËÊB‚_B‚\ÛÜ”İš[™ÜÊÙ^\ÊB‚XYˆHXZÙJ×X]K[ŠÙ^\ÊJ
B‚Y›ÜˆËÙ^HH˜[™ÙHÙ^\ÈÂ‚BXXØİHİ]VÚÙ^WB‚BXYˆHš[˜\KšYÑ[™X[‹\[™Z[ÌŠY‹Z[ÌŠ[ŠÙ^JJJB‚BXYˆH\[™
Y‹×X]JÙ^JK‹‹ŠB‚BXYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹XØİ˜[[˜ÙJB‚BXYˆHš[˜\KšYÑ[™X[‹\[™Z[
Y‹XØİ“›Û˜ÙJB‚_B‚\™]\›ˆÚLM‹”İ[LMŠYŠBŸB‚™[˜È\U˜[œØXİ[ÛœÊ\™[X\Üİš[™×PXØÛİ[İ]KÈ×U˜[œØXİ[Û‹ÛÚ[˜˜\ÙHİš[™Ë™]Ø\™Z[
H
X\Üİš[™×PXØÛİ[İ]K\œ›ÜŠHÂ‚\İ]HHÛÛ™Tİ]J\™[
B‚Y›ÜˆKH˜[™ÙHÈÂ‚BZYˆ•ÈOHˆˆÂ‚BB\™]\›ˆš[›]‘\œ›Ü™Š˜[œØXİ[Ûˆ	YˆÈ\È™\]Z\™Y‹JB‚B_B‚BZYˆ‘œ›ÛHOHˆˆÂ‚BB\™]\›ˆš[›]‘\œ›Ü™Š˜[œØXİ[Ûˆ	Yˆœ›ÛH\È™\]Z\™Y‹JB‚B_B‚BYœ›ÛHHİ]Vİ‘œ›ÛWB‚BZYˆœ›ÛK“›Û˜ÙHOH“›Û˜ÙHÂ‚BB\™]\›ˆš[›]‘\œ›Ü™Š˜[œØXİ[Ûˆ	Yˆ›Û˜ÙHZ\ÛX]ÚÛİIYØ[IY‹K“›Û˜ÙKœ›ÛK“›Û˜ÙJB‚B_B‚BZYˆœ›ÛK˜[[˜ÙH•˜[YHÂ‚BB\™]\›ˆš[›]‘\œ›Ü™Š˜[œØXİ[Ûˆ	Yˆ[œİY™šXÚY[˜[[˜ÙH‹JB‚B_B‚BYœ›ÛK˜[[˜ÙHOH•˜[YB‚BYœ›ÛK“›Û˜ÙJÊÂ‚BZYˆœ›ÛK˜[[˜ÙHOH	‰ˆœ›ÛK“›Û˜ÙHOHÂ‚BBY[]Jİ]K‘œ›ÛJB‚B_H[ÙHÂ‚BB\İ]Vİ‘œ›ÛWHHœ›ÛB‚B_B‚B]ÈHİ]Vİ•×B‚B]Ë˜[[˜ÙH
ÏH•˜[YB‚B\İ]Vİ•×HHÂ‚_B‚ZYˆÛÚ[˜˜\ÙHOHˆˆ	‰ˆ™]Ø\™ˆÂ‚BXXØİHİ]VØÛÚ[˜˜\ÙWB‚BXXØİ˜[[˜ÙH
ÏH™]Ø\™‚B\İ]VØÛÚ[˜˜\ÙWHHXØİ‚_B‚\™]\›ˆİ]Kš[ŸB
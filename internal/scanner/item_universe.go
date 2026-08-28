package scanner

// ItemUniverse is the bounded set of item type IDs the Scanner evaluates
// at each Hub (see CONTEXT.md's Item Universe definition). Every ID here
// was verified live against ESI's GET /universe/types/{id}/, not
// recalled from memory -- see docs/adr/0007-item-universe-starter-list.md
// for why this starter list is small and how to extend it.
var ItemUniverse = []int32{
	34,    // Tritanium
	35,    // Pyerite
	36,    // Mexallon
	37,    // Isogen
	38,    // Nocxium
	39,    // Zydrine
	40,    // Megacyte
	11399, // Morphite
	44992, // PLEX
}

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

	// Ammunition
	193,   // EMP M
	230,   // Antimatter Charge M
	254,   // Multifrequency M
	12773, // Barrage M
	21896, // Republic Fleet EMP M
	23025, // Caldari Navy Antimatter Charge M

	// Ships
	587,   // Rifter
	585,   // Slasher
	603,   // Merlin
	602,   // Kestrel
	597,   // Punisher
	589,   // Executioner
	16240, // Catalyst
	32880, // Venture
	17478, // Retriever
	626,   // Vexor
	627,   // Thorax
	621,   // Caracal

	// Modules
	2048,  // Damage Control II
	438,   // 1MN Afterburner II
	3831,  // Medium Shield Extender II
	380,   // Small Shield Extender II
	519,   // Gyrostabilizer II
	22291, // Ballistic Control System II
	10190, // Magnetic Field Stabilizer II
	2410,  // Heavy Missile Launcher II
	10631, // Rocket Launcher II

	// Drones
	2488, // Warrior II
	2456, // Hobgoblin II

	// Materials/components
	28668, // Nanite Repair Paste
	40520, // Large Skill Injector
}

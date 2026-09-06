package gw

import (
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Container windows. The world opens a Java menu by canonical id; the ones
// with a Bedrock container of the same shape — the chest-shaped generic
// rows, the 3×3 bin, the hopper, the shulker box, the furnace family and
// the brewing stand — open at the block the player last used, with the
// container's slots laid out where Bedrock keeps them and the player's own
// below. Menus with no Bedrock twin are closed straight back so the world
// does not wait on them.

// menuWindow describes a canonical menu Bedrock can show: the Bedrock home
// of each container slot, indexed by Java slot, and the container type.
type menuWindow struct {
	layout []winSlot
	ctype  byte
}

// furnaceLayout: input, fuel, result — Bedrock's furnace keeps them under
// their own container names at the same indices.
var furnaceLayout = []winSlot{
	{protocol.ContainerFurnaceIngredient, 0},
	{protocol.ContainerFurnaceFuel, 1},
	{protocol.ContainerFurnaceResult, 2},
}

// brewingLayout: Java's three bottles, ingredient, fuel are Bedrock's
// ingredient at 0, bottles at 1-3, fuel at 4.
var brewingLayout = []winSlot{
	{protocol.ContainerBrewingStandResult, 1},
	{protocol.ContainerBrewingStandResult, 2},
	{protocol.ContainerBrewingStandResult, 3},
	{protocol.ContainerBrewingStandInput, 0},
	{protocol.ContainerBrewingStandFuel, 4},
}

// craftingLayout: the result, then the 3x3 grid — both in Bedrock's UI
// window (the crafting table's own window holds nothing).
var craftingLayout = func() []winSlot {
	l := []winSlot{{protocol.ContainerCraftingOutputPreview, craftOutputSlot}}
	for i := uint32(0); i < 9; i++ {
		l = append(l, winSlot{protocol.ContainerCraftingInput, tableGridFirst + i})
	}
	return l
}()

// anvilLayout and grindstoneLayout: two inputs and the result, in Bedrock's
// UI window at the slots Geyser established.
var (
	anvilLayout = []winSlot{
		{protocol.ContainerAnvilInput, 1},
		{protocol.ContainerAnvilMaterial, 2},
		{protocol.ContainerAnvilResultPreview, craftOutputSlot},
	}
	grindstoneLayout = []winSlot{
		{protocol.ContainerGrindstoneInput, 16},
		{protocol.ContainerGrindstoneAdditional, 17},
		{protocol.ContainerGrindstoneResultPreview, craftOutputSlot},
	}
)

// tradeLayout: Bedrock's trade screen has two ingredient slots and the
// result, in the UI window.
var tradeLayout = []winSlot{
	{protocol.ContainerTradeTwoIngredientOne, 4},
	{protocol.ContainerTradeTwoIngredientTwo, 5},
	{protocol.ContainerTradeTwoResultPreview, craftOutputSlot},
}

// enchantLayout: the item and the lapis, in the UI window.
var enchantLayout = []winSlot{
	{protocol.ContainerEnchantingInput, 14},
	{protocol.ContainerEnchantingMaterial, 15},
}

// stonecutterLayout: the input and the result, in the UI window.
var stonecutterLayout = []winSlot{
	{protocol.ContainerStonecutterInput, 3},
	{protocol.ContainerStonecutterResultPreview, craftOutputSlot},
}

// smithingLayout and loomLayout: Java's template/base/addition/result and
// banner/dye/pattern/result, in the UI window.
var (
	smithingLayout = []winSlot{
		{protocol.ContainerSmithingTableTemplate, 53},
		{protocol.ContainerSmithingTableInput, 51},
		{protocol.ContainerSmithingTableMaterial, 52},
		{protocol.ContainerSmithingTableResultPreview, craftOutputSlot},
	}
	loomLayout = []winSlot{
		{protocol.ContainerLoomInput, 9},
		{protocol.ContainerLoomDye, 10},
		{protocol.ContainerLoomMaterial, 11},
		{protocol.ContainerLoomResultPreview, craftOutputSlot},
	}
)

var menuWindows = map[int32]menuWindow{
	0:  {chestLayout(9), protocol.ContainerTypeContainer},      // generic_9x1
	1:  {chestLayout(18), protocol.ContainerTypeContainer},     // generic_9x2
	2:  {chestLayout(27), protocol.ContainerTypeContainer},     // generic_9x3
	3:  {chestLayout(36), protocol.ContainerTypeContainer},     // generic_9x4
	4:  {chestLayout(45), protocol.ContainerTypeContainer},     // generic_9x5
	5:  {chestLayout(54), protocol.ContainerTypeContainer},     // generic_9x6
	6:  {chestLayout(9), protocol.ContainerTypeDispenser},      // generic_3x3 (dispenser/dropper)
	8:  {anvilLayout, protocol.ContainerTypeAnvil},             // anvil
	10: {furnaceLayout, protocol.ContainerTypeBlastFurnace},    // blast_furnace
	11: {brewingLayout, protocol.ContainerTypeBrewingStand},    // brewing_stand
	12: {craftingLayout, protocol.ContainerTypeWorkbench},      // crafting
	13: {enchantLayout, protocol.ContainerTypeEnchantment},     // enchantment
	14: {furnaceLayout, protocol.ContainerTypeFurnace},         // furnace
	15: {grindstoneLayout, protocol.ContainerTypeGrindstone},   // grindstone
	16: {chestLayout(5), protocol.ContainerTypeHopper},         // hopper
	18: {loomLayout, protocol.ContainerTypeLoom},               // loom
	19: {tradeLayout, protocol.ContainerTypeTrade},             // merchant
	20: {chestLayout(27), protocol.ContainerTypeContainer},     // shulker_box
	21: {smithingLayout, protocol.ContainerTypeSmithingTable},  // smithing
	24: {stonecutterLayout, protocol.ContainerTypeStonecutter}, // stonecutter
	22: {furnaceLayout, protocol.ContainerTypeSmoker},          // smoker
}

// winState is the container window open for this client, shared between
// the world-side and client-side goroutines.
type winState struct {
	mu     sync.Mutex
	mirror *invMirror // nil = no container open
	ctype  byte
	title  string
	// lastUse is the block the client last used — where the window sits.
	lastUse [3]int32
	// lastEntity is the entity the client last used — a trade screen's villager.
	lastEntity int64
}

func (w *winState) open(id int32, layout []winSlot, ctype byte, title string) *invMirror {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.mirror = newWindowMirror(id, layout)
	w.mirror.table = ctype == protocol.ContainerTypeEnchantment
	w.mirror.cutter = ctype == protocol.ContainerTypeStonecutter
	w.mirror.smith = ctype == protocol.ContainerTypeSmithingTable
	w.mirror.loom = ctype == protocol.ContainerTypeLoom
	for i := range w.mirror.ench {
		w.mirror.ench[i] = enchantRow{bedrock: -1, level: -1}
	}
	w.ctype = ctype
	w.title = title
	return w.mirror
}

// currentTitle is the open window's title (the trade screen's heading).
func (w *winState) currentTitle() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.title
}

func (w *winState) usedEntity(id int64) {
	w.mu.Lock()
	w.lastEntity = id
	w.mu.Unlock()
}

func (w *winState) usedEntityID() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastEntity
}

// current returns the open window's mirror (nil when none).
func (w *winState) current() *invMirror {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mirror
}

// currentType returns the open window's mirror and container type.
func (w *winState) currentType() (*invMirror, byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mirror, w.ctype
}

func (w *winState) close() (id int32, ctype byte, was bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.mirror == nil {
		return 0, 0, false
	}
	id, ctype = w.mirror.window, w.ctype
	w.mirror = nil
	return id, ctype, true
}

func (w *winState) used(x, y, z int32) {
	w.mu.Lock()
	w.lastUse = [3]int32{x, y, z}
	w.mu.Unlock()
}

func (w *winState) usedAt() [3]int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUse
}

// sendWindowItems renders a container window: its own slots where Bedrock
// keeps them, then the player's inventory as the window's lower half.
func sendWindowItems(w packetWriter, m *invMirror, slots []attach.ItemStack) {
	if len(m.layout) > 0 && uiContainer(m.layout[0].container) {
		for j := range m.layout { // UI slots are set one by one
			if j < len(slots) {
				sendWindowSlot(w, m, int32(j), slots[j])
			}
		}
	} else {
		content := make([]protocol.ItemInstance, len(m.layout))
		for j, ws := range m.layout {
			if j < len(slots) && int(ws.idx) < len(content) {
				content[ws.idx] = bedrockStack(slots[j])
			}
		}
		w.WritePacket(&packet.InventoryContent{
			WindowID:  uint32(m.window),
			Content:   content,
			Container: fullContainer(protocol.ContainerLevelEntity),
		})
	}
	sendPlayerInventory(w, m.playerView())
}

// sendWindowSlot renders one changed slot of a container window.
func sendWindowSlot(w packetWriter, m *invMirror, slot int32, st attach.ItemStack) {
	if int(slot) < len(m.layout) {
		ws := m.layout[slot]
		win := uint32(m.window)
		if uiContainer(ws.container) {
			win = protocol.WindowIDUI
		}
		w.WritePacket(&packet.InventorySlot{
			WindowID:  win,
			Slot:      ws.idx,
			NewItem:   bedrockStack(st),
			Container: protocol.Option(fullContainer(ws.container)),
		})
		return
	}
	if _, idx, ok := m.mapOut(slot); ok {
		// Bedrock's inventory index: hotbar 0-8 (Java 36-44), main 9-35 (Java 9-35).
		if idx < 9 {
			sendInventorySlot(w, javaHotbarFirst+int32(idx), st)
		} else {
			sendInventorySlot(w, int32(idx), st)
		}
	}
}

// windowData relays a Java menu property (the furnace's burn and cook bars,
// the brewing stand's brew time and fuel) as Bedrock's container data. The
// furnace's total cook time has no Bedrock key: its client assumes the
// container type's own pace.
func windowData(w packetWriter, m *invMirror, ctype byte, prop, value int32) {
	var key int32
	switch ctype {
	case protocol.ContainerTypeEnchantment:
		if m.enchantProp(prop, value) {
			w.WritePacket(m.enchantOptions())
		}
		return
	case protocol.ContainerTypeFurnace, protocol.ContainerTypeBlastFurnace, protocol.ContainerTypeSmoker:
		switch prop {
		case 0:
			key = packet.ContainerDataFurnaceLitTime
		case 1:
			key = packet.ContainerDataFurnaceLitDuration
		case 2:
			key = packet.ContainerDataFurnaceTickCount
		default:
			return
		}
	case protocol.ContainerTypeBrewingStand:
		switch prop {
		case 0:
			key = packet.ContainerDataBrewingStandBrewTime
		case 1:
			key = packet.ContainerDataBrewingStandFuelAmount
		default:
			return
		}
	default:
		return
	}
	w.WritePacket(&packet.ContainerSetData{WindowID: byte(m.window), Key: key, Value: value})
}

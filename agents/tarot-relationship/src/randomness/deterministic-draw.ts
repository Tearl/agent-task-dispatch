import { createHash } from "node:crypto";
import { DECK_VERSION, TAROT_DECK } from "../domain/cards.ts";
import type { DrawProof, DrawnCard, Orientation } from "../domain/types.ts";

export const SPREAD_VERSION = "relationship-mirror-3-v1" as const;
export const DRAW_ALGORITHM = "sha256-counter-fisher-yates-v1" as const;

const positions = ["当前关系能量", "核心互动模式或阻碍", "你可以采取的行动"] as const;

interface DrawScope {
  taskSpecHash: string;
  scope: "overview" | "assignment";
  scopeId: string;
}

export function drawRelationshipSpread(scope: DrawScope): { cards: DrawnCard[]; proof: DrawProof } {
  const seedMaterial = [scope.taskSpecHash, scope.scope, scope.scopeId, DECK_VERSION, SPREAD_VERSION].join("\n");
  const seed = createHash("sha256").update(seedMaterial).digest();
  const random = new HashRandom(seed);
  const indexes = Array.from({ length: TAROT_DECK.length }, (_, index) => index);

  for (let index = indexes.length - 1; index > 0; index--) {
    const target = random.integer(index + 1);
    [indexes[index], indexes[target]] = [indexes[target]!, indexes[index]!];
  }

  const cards = indexes.slice(0, positions.length).map((deckIndex, index) => {
    const card = TAROT_DECK[deckIndex];
    if (!card) throw new Error("draw selected an unknown card");
    const orientation: Orientation = random.integer(2) === 0 ? "upright" : "reversed";
    return {
      ...card,
      position: positions[index]!,
      orientation,
      baseMeaning: orientation === "upright" ? card.uprightMeaning : card.reversedMeaning,
    };
  });

  return {
    cards,
    proof: {
      algorithm: DRAW_ALGORITHM,
      deckVersion: DECK_VERSION,
      spreadVersion: SPREAD_VERSION,
      scope: scope.scope,
      scopeId: scope.scopeId,
      seedDigest: `sha256:${seed.toString("hex")}`,
    },
  };
}

class HashRandom {
  private readonly seed: Buffer;
  private counter = 0;
  private buffer = Buffer.alloc(0);
  private offset = 0;

  constructor(seed: Buffer) {
    this.seed = seed;
  }

  integer(maximumExclusive: number): number {
    if (!Number.isSafeInteger(maximumExclusive) || maximumExclusive < 1 || maximumExclusive > 0x1_0000_0000) {
      throw new Error("random integer bound is invalid");
    }
    const range = 0x1_0000_0000;
    const limit = Math.floor(range / maximumExclusive) * maximumExclusive;
    let value: number;
    do {
      value = this.uint32();
    } while (value >= limit);
    return value % maximumExclusive;
  }

  private uint32(): number {
    if (this.offset + 4 > this.buffer.length) {
      const counter = Buffer.allocUnsafe(8);
      counter.writeBigUInt64BE(BigInt(this.counter++));
      this.buffer = createHash("sha256").update(this.seed).update(counter).digest();
      this.offset = 0;
    }
    const value = this.buffer.readUInt32BE(this.offset);
    this.offset += 4;
    return value;
  }
}

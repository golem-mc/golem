// Code generated from the block and registry reports of a vanilla 26.3 server. DO NOT EDIT.

package registry

// Block is a block's protocol ID in the minecraft:block registry.
type Block uint16

// BlockCount is the number of blocks; every Block is below it.
const BlockCount = 1286

// StateCount is the number of block states; every state ID is below it.
const StateCount = 35723

// The blocks, one constant per registry entry.
const (
	BlockAir                             Block = 0
	BlockStone                           Block = 1
	BlockGranite                         Block = 2
	BlockPolishedGranite                 Block = 3
	BlockDiorite                         Block = 4
	BlockPolishedDiorite                 Block = 5
	BlockAndesite                        Block = 6
	BlockPolishedAndesite                Block = 7
	BlockGrassBlock                      Block = 8
	BlockDirt                            Block = 9
	BlockCoarseDirt                      Block = 10
	BlockPodzol                          Block = 11
	BlockCobblestone                     Block = 12
	BlockOakPlanks                       Block = 13
	BlockSprucePlanks                    Block = 14
	BlockBirchPlanks                     Block = 15
	BlockJunglePlanks                    Block = 16
	BlockAcaciaPlanks                    Block = 17
	BlockCherryPlanks                    Block = 18
	BlockDarkOakPlanks                   Block = 19
	BlockPaleOakWood                     Block = 20
	BlockPaleOakPlanks                   Block = 21
	BlockMangrovePlanks                  Block = 22
	BlockPoplarPlanks                    Block = 23
	BlockBambooPlanks                    Block = 24
	BlockBambooMosaic                    Block = 25
	BlockOakSapling                      Block = 26
	BlockSpruceSapling                   Block = 27
	BlockBirchSapling                    Block = 28
	BlockJungleSapling                   Block = 29
	BlockAcaciaSapling                   Block = 30
	BlockCherrySapling                   Block = 31
	BlockDarkOakSapling                  Block = 32
	BlockPaleOakSapling                  Block = 33
	BlockMangrovePropagule               Block = 34
	BlockPoplarSapling                   Block = 35
	BlockBedrock                         Block = 36
	BlockWater                           Block = 37
	BlockLava                            Block = 38
	BlockSand                            Block = 39
	BlockSuspiciousSand                  Block = 40
	BlockRedSand                         Block = 41
	BlockGravel                          Block = 42
	BlockSuspiciousGravel                Block = 43
	BlockGoldOre                         Block = 44
	BlockDeepslateGoldOre                Block = 45
	BlockIronOre                         Block = 46
	BlockDeepslateIronOre                Block = 47
	BlockCoalOre                         Block = 48
	BlockDeepslateCoalOre                Block = 49
	BlockNetherGoldOre                   Block = 50
	BlockOakLog                          Block = 51
	BlockSpruceLog                       Block = 52
	BlockBirchLog                        Block = 53
	BlockJungleLog                       Block = 54
	BlockAcaciaLog                       Block = 55
	BlockCherryLog                       Block = 56
	BlockDarkOakLog                      Block = 57
	BlockPaleOakLog                      Block = 58
	BlockMangroveLog                     Block = 59
	BlockPoplarLog                       Block = 60
	BlockMangroveRoots                   Block = 61
	BlockMuddyMangroveRoots              Block = 62
	BlockBambooBlock                     Block = 63
	BlockStrippedSpruceLog               Block = 64
	BlockStrippedBirchLog                Block = 65
	BlockStrippedJungleLog               Block = 66
	BlockStrippedAcaciaLog               Block = 67
	BlockStrippedCherryLog               Block = 68
	BlockStrippedDarkOakLog              Block = 69
	BlockStrippedPaleOakLog              Block = 70
	BlockStrippedOakLog                  Block = 71
	BlockStrippedMangroveLog             Block = 72
	BlockStrippedPoplarLog               Block = 73
	BlockStrippedBambooBlock             Block = 74
	BlockOakWood                         Block = 75
	BlockSpruceWood                      Block = 76
	BlockBirchWood                       Block = 77
	BlockJungleWood                      Block = 78
	BlockAcaciaWood                      Block = 79
	BlockCherryWood                      Block = 80
	BlockDarkOakWood                     Block = 81
	BlockMangroveWood                    Block = 82
	BlockPoplarWood                      Block = 83
	BlockStrippedOakWood                 Block = 84
	BlockStrippedSpruceWood              Block = 85
	BlockStrippedBirchWood               Block = 86
	BlockStrippedJungleWood              Block = 87
	BlockStrippedAcaciaWood              Block = 88
	BlockStrippedCherryWood              Block = 89
	BlockStrippedDarkOakWood             Block = 90
	BlockStrippedPaleOakWood             Block = 91
	BlockStrippedMangroveWood            Block = 92
	BlockStrippedPoplarWood              Block = 93
	BlockOakLeaves                       Block = 94
	BlockSpruceLeaves                    Block = 95
	BlockBirchLeaves                     Block = 96
	BlockJungleLeaves                    Block = 97
	BlockAcaciaLeaves                    Block = 98
	BlockCherryLeaves                    Block = 99
	BlockDarkOakLeaves                   Block = 100
	BlockPaleOakLeaves                   Block = 101
	BlockMangroveLeaves                  Block = 102
	BlockRedPoplarLeaves                 Block = 103
	BlockOrangePoplarLeaves              Block = 104
	BlockYellowPoplarLeaves              Block = 105
	BlockAzaleaLeaves                    Block = 106
	BlockFloweringAzaleaLeaves           Block = 107
	BlockSponge                          Block = 108
	BlockWetSponge                       Block = 109
	BlockGlass                           Block = 110
	BlockLapisOre                        Block = 111
	BlockDeepslateLapisOre               Block = 112
	BlockLapisBlock                      Block = 113
	BlockDispenser                       Block = 114
	BlockSandstone                       Block = 115
	BlockChiseledSandstone               Block = 116
	BlockCutSandstone                    Block = 117
	BlockNoteBlock                       Block = 118
	BlockWhiteBed                        Block = 119
	BlockOrangeBed                       Block = 120
	BlockMagentaBed                      Block = 121
	BlockLightBlueBed                    Block = 122
	BlockYellowBed                       Block = 123
	BlockLimeBed                         Block = 124
	BlockPinkBed                         Block = 125
	BlockGrayBed                         Block = 126
	BlockLightGrayBed                    Block = 127
	BlockCyanBed                         Block = 128
	BlockPurpleBed                       Block = 129
	BlockBlueBed                         Block = 130
	BlockBrownBed                        Block = 131
	BlockGreenBed                        Block = 132
	BlockRedBed                          Block = 133
	BlockBlackBed                        Block = 134
	BlockStrawBed                        Block = 135
	BlockPoweredRail                     Block = 136
	BlockDetectorRail                    Block = 137
	BlockStickyPiston                    Block = 138
	BlockCobweb                          Block = 139
	BlockShortGrass                      Block = 140
	BlockFern                            Block = 141
	BlockDeadBush                        Block = 142
	BlockBush                            Block = 143
	BlockRedShrub                        Block = 144
	BlockShortDryGrass                   Block = 145
	BlockTallDryGrass                    Block = 146
	BlockSeagrass                        Block = 147
	BlockTallSeagrass                    Block = 148
	BlockPiston                          Block = 149
	BlockPistonHead                      Block = 150
	BlockWhiteWool                       Block = 151
	BlockOrangeWool                      Block = 152
	BlockMagentaWool                     Block = 153
	BlockLightBlueWool                   Block = 154
	BlockYellowWool                      Block = 155
	BlockLimeWool                        Block = 156
	BlockPinkWool                        Block = 157
	BlockGrayWool                        Block = 158
	BlockLightGrayWool                   Block = 159
	BlockCyanWool                        Block = 160
	BlockPurpleWool                      Block = 161
	BlockBlueWool                        Block = 162
	BlockBrownWool                       Block = 163
	BlockGreenWool                       Block = 164
	BlockRedWool                         Block = 165
	BlockBlackWool                       Block = 166
	BlockWhiteWoolStairs                 Block = 167
	BlockOrangeWoolStairs                Block = 168
	BlockMagentaWoolStairs               Block = 169
	BlockLightBlueWoolStairs             Block = 170
	BlockYellowWoolStairs                Block = 171
	BlockLimeWoolStairs                  Block = 172
	BlockPinkWoolStairs                  Block = 173
	BlockGrayWoolStairs                  Block = 174
	BlockLightGrayWoolStairs             Block = 175
	BlockCyanWoolStairs                  Block = 176
	BlockPurpleWoolStairs                Block = 177
	BlockBlueWoolStairs                  Block = 178
	BlockBrownWoolStairs                 Block = 179
	BlockGreenWoolStairs                 Block = 180
	BlockRedWoolStairs                   Block = 181
	BlockBlackWoolStairs                 Block = 182
	BlockWhiteWoolSlab                   Block = 183
	BlockOrangeWoolSlab                  Block = 184
	BlockMagentaWoolSlab                 Block = 185
	BlockLightBlueWoolSlab               Block = 186
	BlockYellowWoolSlab                  Block = 187
	BlockLimeWoolSlab                    Block = 188
	BlockPinkWoolSlab                    Block = 189
	BlockGrayWoolSlab                    Block = 190
	BlockLightGrayWoolSlab               Block = 191
	BlockCyanWoolSlab                    Block = 192
	BlockPurpleWoolSlab                  Block = 193
	BlockBlueWoolSlab                    Block = 194
	BlockBrownWoolSlab                   Block = 195
	BlockGreenWoolSlab                   Block = 196
	BlockRedWoolSlab                     Block = 197
	BlockBlackWoolSlab                   Block = 198
	BlockMovingPiston                    Block = 199
	BlockDandelion                       Block = 200
	BlockGoldenDandelion                 Block = 201
	BlockTorchflower                     Block = 202
	BlockPoppy                           Block = 203
	BlockBlueOrchid                      Block = 204
	BlockAllium                          Block = 205
	BlockAzureBluet                      Block = 206
	BlockRedTulip                        Block = 207
	BlockOrangeTulip                     Block = 208
	BlockWhiteTulip                      Block = 209
	BlockPinkTulip                       Block = 210
	BlockOxeyeDaisy                      Block = 211
	BlockCornflower                      Block = 212
	BlockWitherRose                      Block = 213
	BlockLilyOfTheValley                 Block = 214
	BlockBrownMushroom                   Block = 215
	BlockRedMushroom                     Block = 216
	BlockGoldBlock                       Block = 217
	BlockIronBlock                       Block = 218
	BlockBricks                          Block = 219
	BlockTnt                             Block = 220
	BlockBookshelf                       Block = 221
	BlockChiseledBookshelf               Block = 222
	BlockAcaciaShelf                     Block = 223
	BlockBambooShelf                     Block = 224
	BlockBirchShelf                      Block = 225
	BlockCherryShelf                     Block = 226
	BlockCrimsonShelf                    Block = 227
	BlockDarkOakShelf                    Block = 228
	BlockJungleShelf                     Block = 229
	BlockMangroveShelf                   Block = 230
	BlockOakShelf                        Block = 231
	BlockPaleOakShelf                    Block = 232
	BlockPoplarShelf                     Block = 233
	BlockSpruceShelf                     Block = 234
	BlockWarpedShelf                     Block = 235
	BlockMossyCobblestone                Block = 236
	BlockObsidian                        Block = 237
	BlockTorch                           Block = 238
	BlockWallTorch                       Block = 239
	BlockFire                            Block = 240
	BlockSoulFire                        Block = 241
	BlockSpawner                         Block = 242
	BlockCreakingHeart                   Block = 243
	BlockOakStairs                       Block = 244
	BlockChest                           Block = 245
	BlockRedstoneWire                    Block = 246
	BlockDiamondOre                      Block = 247
	BlockDeepslateDiamondOre             Block = 248
	BlockDiamondBlock                    Block = 249
	BlockCraftingTable                   Block = 250
	BlockWheat                           Block = 251
	BlockFarmland                        Block = 252
	BlockFurnace                         Block = 253
	BlockOakSign                         Block = 254
	BlockSpruceSign                      Block = 255
	BlockBirchSign                       Block = 256
	BlockAcaciaSign                      Block = 257
	BlockCherrySign                      Block = 258
	BlockJungleSign                      Block = 259
	BlockDarkOakSign                     Block = 260
	BlockPaleOakSign                     Block = 261
	BlockMangroveSign                    Block = 262
	BlockPoplarSign                      Block = 263
	BlockBambooSign                      Block = 264
	BlockOakDoor                         Block = 265
	BlockLadder                          Block = 266
	BlockRail                            Block = 267
	BlockCobblestoneStairs               Block = 268
	BlockOakWallSign                     Block = 269
	BlockSpruceWallSign                  Block = 270
	BlockBirchWallSign                   Block = 271
	BlockAcaciaWallSign                  Block = 272
	BlockCherryWallSign                  Block = 273
	BlockJungleWallSign                  Block = 274
	BlockDarkOakWallSign                 Block = 275
	BlockPaleOakWallSign                 Block = 276
	BlockMangroveWallSign                Block = 277
	BlockPoplarWallSign                  Block = 278
	BlockBambooWallSign                  Block = 279
	BlockOakHangingSign                  Block = 280
	BlockSpruceHangingSign               Block = 281
	BlockBirchHangingSign                Block = 282
	BlockAcaciaHangingSign               Block = 283
	BlockCherryHangingSign               Block = 284
	BlockJungleHangingSign               Block = 285
	BlockDarkOakHangingSign              Block = 286
	BlockPaleOakHangingSign              Block = 287
	BlockCrimsonHangingSign              Block = 288
	BlockWarpedHangingSign               Block = 289
	BlockMangroveHangingSign             Block = 290
	BlockPoplarHangingSign               Block = 291
	BlockBambooHangingSign               Block = 292
	BlockOakWallHangingSign              Block = 293
	BlockSpruceWallHangingSign           Block = 294
	BlockBirchWallHangingSign            Block = 295
	BlockAcaciaWallHangingSign           Block = 296
	BlockCherryWallHangingSign           Block = 297
	BlockJungleWallHangingSign           Block = 298
	BlockDarkOakWallHangingSign          Block = 299
	BlockPaleOakWallHangingSign          Block = 300
	BlockMangroveWallHangingSign         Block = 301
	BlockPoplarWallHangingSign           Block = 302
	BlockCrimsonWallHangingSign          Block = 303
	BlockWarpedWallHangingSign           Block = 304
	BlockBambooWallHangingSign           Block = 305
	BlockLever                           Block = 306
	BlockStonePressurePlate              Block = 307
	BlockIronDoor                        Block = 308
	BlockOakPressurePlate                Block = 309
	BlockSprucePressurePlate             Block = 310
	BlockBirchPressurePlate              Block = 311
	BlockJunglePressurePlate             Block = 312
	BlockAcaciaPressurePlate             Block = 313
	BlockCherryPressurePlate             Block = 314
	BlockDarkOakPressurePlate            Block = 315
	BlockPaleOakPressurePlate            Block = 316
	BlockMangrovePressurePlate           Block = 317
	BlockPoplarPressurePlate             Block = 318
	BlockBambooPressurePlate             Block = 319
	BlockRedstoneOre                     Block = 320
	BlockDeepslateRedstoneOre            Block = 321
	BlockRedstoneTorch                   Block = 322
	BlockRedstoneWallTorch               Block = 323
	BlockStoneButton                     Block = 324
	BlockSnow                            Block = 325
	BlockIce                             Block = 326
	BlockSnowBlock                       Block = 327
	BlockCactus                          Block = 328
	BlockCactusFlower                    Block = 329
	BlockClay                            Block = 330
	BlockSugarCane                       Block = 331
	BlockJukebox                         Block = 332
	BlockOakFence                        Block = 333
	BlockNetherrack                      Block = 334
	BlockSoulSand                        Block = 335
	BlockSoulSoil                        Block = 336
	BlockBasalt                          Block = 337
	BlockPolishedBasalt                  Block = 338
	BlockSoulTorch                       Block = 339
	BlockSoulWallTorch                   Block = 340
	BlockCopperTorch                     Block = 341
	BlockCopperWallTorch                 Block = 342
	BlockGlowstone                       Block = 343
	BlockNetherPortal                    Block = 344
	BlockCarvedPumpkin                   Block = 345
	BlockJackOLantern                    Block = 346
	BlockCake                            Block = 347
	BlockRepeater                        Block = 348
	BlockWhiteStainedGlass               Block = 349
	BlockOrangeStainedGlass              Block = 350
	BlockMagentaStainedGlass             Block = 351
	BlockLightBlueStainedGlass           Block = 352
	BlockYellowStainedGlass              Block = 353
	BlockLimeStainedGlass                Block = 354
	BlockPinkStainedGlass                Block = 355
	BlockGrayStainedGlass                Block = 356
	BlockLightGrayStainedGlass           Block = 357
	BlockCyanStainedGlass                Block = 358
	BlockPurpleStainedGlass              Block = 359
	BlockBlueStainedGlass                Block = 360
	BlockBrownStainedGlass               Block = 361
	BlockGreenStainedGlass               Block = 362
	BlockRedStainedGlass                 Block = 363
	BlockBlackStainedGlass               Block = 364
	BlockOakTrapdoor                     Block = 365
	BlockSpruceTrapdoor                  Block = 366
	BlockBirchTrapdoor                   Block = 367
	BlockJungleTrapdoor                  Block = 368
	BlockAcaciaTrapdoor                  Block = 369
	BlockCherryTrapdoor                  Block = 370
	BlockDarkOakTrapdoor                 Block = 371
	BlockPaleOakTrapdoor                 Block = 372
	BlockMangroveTrapdoor                Block = 373
	BlockPoplarTrapdoor                  Block = 374
	BlockBambooTrapdoor                  Block = 375
	BlockStoneBricks                     Block = 376
	BlockMossyStoneBricks                Block = 377
	BlockCrackedStoneBricks              Block = 378
	BlockChiseledStoneBricks             Block = 379
	BlockPackedMud                       Block = 380
	BlockMudBricks                       Block = 381
	BlockInfestedStone                   Block = 382
	BlockInfestedCobblestone             Block = 383
	BlockInfestedStoneBricks             Block = 384
	BlockInfestedMossyStoneBricks        Block = 385
	BlockInfestedCrackedStoneBricks      Block = 386
	BlockInfestedChiseledStoneBricks     Block = 387
	BlockBrownMushroomBlock              Block = 388
	BlockRedMushroomBlock                Block = 389
	BlockMushroomStem                    Block = 390
	BlockIronBars                        Block = 391
	BlockCopperBars                      Block = 392
	BlockExposedCopperBars               Block = 393
	BlockWeatheredCopperBars             Block = 394
	BlockOxidizedCopperBars              Block = 395
	BlockWaxedCopperBars                 Block = 396
	BlockWaxedExposedCopperBars          Block = 397
	BlockWaxedWeatheredCopperBars        Block = 398
	BlockWaxedOxidizedCopperBars         Block = 399
	BlockIronChain                       Block = 400
	BlockCopperChain                     Block = 401
	BlockExposedCopperChain              Block = 402
	BlockWeatheredCopperChain            Block = 403
	BlockOxidizedCopperChain             Block = 404
	BlockWaxedCopperChain                Block = 405
	BlockWaxedExposedCopperChain         Block = 406
	BlockWaxedWeatheredCopperChain       Block = 407
	BlockWaxedOxidizedCopperChain        Block = 408
	BlockGlassPane                       Block = 409
	BlockPumpkin                         Block = 410
	BlockMelon                           Block = 411
	BlockAttachedPumpkinStem             Block = 412
	BlockAttachedMelonStem               Block = 413
	BlockPumpkinStem                     Block = 414
	BlockMelonStem                       Block = 415
	BlockVine                            Block = 416
	BlockGlowLichen                      Block = 417
	BlockResinClump                      Block = 418
	BlockOakFenceGate                    Block = 419
	BlockBrickStairs                     Block = 420
	BlockStoneBrickStairs                Block = 421
	BlockMudBrickStairs                  Block = 422
	BlockMycelium                        Block = 423
	BlockLilyPad                         Block = 424
	BlockResinBlock                      Block = 425
	BlockResinBricks                     Block = 426
	BlockResinBrickStairs                Block = 427
	BlockResinBrickSlab                  Block = 428
	BlockResinBrickWall                  Block = 429
	BlockChiseledResinBricks             Block = 430
	BlockNetherBricks                    Block = 431
	BlockNetherBrickFence                Block = 432
	BlockNetherBrickStairs               Block = 433
	BlockNetherWart                      Block = 434
	BlockEnchantingTable                 Block = 435
	BlockBrewingStand                    Block = 436
	BlockCauldron                        Block = 437
	BlockWaterCauldron                   Block = 438
	BlockLavaCauldron                    Block = 439
	BlockPowderSnowCauldron              Block = 440
	BlockEndPortal                       Block = 441
	BlockEndPortalFrame                  Block = 442
	BlockEndStone                        Block = 443
	BlockDragonEgg                       Block = 444
	BlockRedstoneLamp                    Block = 445
	BlockCocoa                           Block = 446
	BlockShelfMushroom                   Block = 447
	BlockSandstoneStairs                 Block = 448
	BlockEmeraldOre                      Block = 449
	BlockDeepslateEmeraldOre             Block = 450
	BlockEnderChest                      Block = 451
	BlockTripwireHook                    Block = 452
	BlockTripwire                        Block = 453
	BlockEmeraldBlock                    Block = 454
	BlockSpruceStairs                    Block = 455
	BlockBirchStairs                     Block = 456
	BlockJungleStairs                    Block = 457
	BlockCommandBlock                    Block = 458
	BlockBeacon                          Block = 459
	BlockCobblestoneWall                 Block = 460
	BlockMossyCobblestoneWall            Block = 461
	BlockFlowerPot                       Block = 462
	BlockPottedTorchflower               Block = 463
	BlockPottedOakSapling                Block = 464
	BlockPottedSpruceSapling             Block = 465
	BlockPottedBirchSapling              Block = 466
	BlockPottedJungleSapling             Block = 467
	BlockPottedAcaciaSapling             Block = 468
	BlockPottedCherrySapling             Block = 469
	BlockPottedDarkOakSapling            Block = 470
	BlockPottedPaleOakSapling            Block = 471
	BlockPottedPoplarSapling             Block = 472
	BlockPottedMangrovePropagule         Block = 473
	BlockPottedFern                      Block = 474
	BlockPottedDandelion                 Block = 475
	BlockPottedGoldenDandelion           Block = 476
	BlockPottedPoppy                     Block = 477
	BlockPottedBlueOrchid                Block = 478
	BlockPottedAllium                    Block = 479
	BlockPottedAzureBluet                Block = 480
	BlockPottedRedTulip                  Block = 481
	BlockPottedOrangeTulip               Block = 482
	BlockPottedWhiteTulip                Block = 483
	BlockPottedPinkTulip                 Block = 484
	BlockPottedOxeyeDaisy                Block = 485
	BlockPottedCornflower                Block = 486
	BlockPottedLilyOfTheValley           Block = 487
	BlockPottedWitherRose                Block = 488
	BlockPottedRedMushroom               Block = 489
	BlockPottedBrownMushroom             Block = 490
	BlockPottedDeadBush                  Block = 491
	BlockPottedCactus                    Block = 492
	BlockCarrots                         Block = 493
	BlockPotatoes                        Block = 494
	BlockOakButton                       Block = 495
	BlockSpruceButton                    Block = 496
	BlockBirchButton                     Block = 497
	BlockJungleButton                    Block = 498
	BlockAcaciaButton                    Block = 499
	BlockCherryButton                    Block = 500
	BlockDarkOakButton                   Block = 501
	BlockPaleOakButton                   Block = 502
	BlockMangroveButton                  Block = 503
	BlockPoplarButton                    Block = 504
	BlockBambooButton                    Block = 505
	BlockSkeletonSkull                   Block = 506
	BlockSkeletonWallSkull               Block = 507
	BlockWitherSkeletonSkull             Block = 508
	BlockWitherSkeletonWallSkull         Block = 509
	BlockZombieHead                      Block = 510
	BlockZombieWallHead                  Block = 511
	BlockPlayerHead                      Block = 512
	BlockPlayerWallHead                  Block = 513
	BlockCreeperHead                     Block = 514
	BlockCreeperWallHead                 Block = 515
	BlockDragonHead                      Block = 516
	BlockDragonWallHead                  Block = 517
	BlockPiglinHead                      Block = 518
	BlockPiglinWallHead                  Block = 519
	BlockAnvil                           Block = 520
	BlockChippedAnvil                    Block = 521
	BlockDamagedAnvil                    Block = 522
	BlockTrappedChest                    Block = 523
	BlockLightWeightedPressurePlate      Block = 524
	BlockHeavyWeightedPressurePlate      Block = 525
	BlockComparator                      Block = 526
	BlockDaylightDetector                Block = 527
	BlockRedstoneBlock                   Block = 528
	BlockNetherQuartzOre                 Block = 529
	BlockHopper                          Block = 530
	BlockQuartzBlock                     Block = 531
	BlockChiseledQuartzBlock             Block = 532
	BlockQuartzPillar                    Block = 533
	BlockQuartzStairs                    Block = 534
	BlockActivatorRail                   Block = 535
	BlockDropper                         Block = 536
	BlockWhiteTerracotta                 Block = 537
	BlockOrangeTerracotta                Block = 538
	BlockMagentaTerracotta               Block = 539
	BlockLightBlueTerracotta             Block = 540
	BlockYellowTerracotta                Block = 541
	BlockLimeTerracotta                  Block = 542
	BlockPinkTerracotta                  Block = 543
	BlockGrayTerracotta                  Block = 544
	BlockLightGrayTerracotta             Block = 545
	BlockCyanTerracotta                  Block = 546
	BlockPurpleTerracotta                Block = 547
	BlockBlueTerracotta                  Block = 548
	BlockBrownTerracotta                 Block = 549
	BlockGreenTerracotta                 Block = 550
	BlockRedTerracotta                   Block = 551
	BlockBlackTerracotta                 Block = 552
	BlockWhiteStainedGlassPane           Block = 553
	BlockOrangeStainedGlassPane          Block = 554
	BlockMagentaStainedGlassPane         Block = 555
	BlockLightBlueStainedGlassPane       Block = 556
	BlockYellowStainedGlassPane          Block = 557
	BlockLimeStainedGlassPane            Block = 558
	BlockPinkStainedGlassPane            Block = 559
	BlockGrayStainedGlassPane            Block = 560
	BlockLightGrayStainedGlassPane       Block = 561
	BlockCyanStainedGlassPane            Block = 562
	BlockPurpleStainedGlassPane          Block = 563
	BlockBlueStainedGlassPane            Block = 564
	BlockBrownStainedGlassPane           Block = 565
	BlockGreenStainedGlassPane           Block = 566
	BlockRedStainedGlassPane             Block = 567
	BlockBlackStainedGlassPane           Block = 568
	BlockAcaciaStairs                    Block = 569
	BlockCherryStairs                    Block = 570
	BlockDarkOakStairs                   Block = 571
	BlockPaleOakStairs                   Block = 572
	BlockMangroveStairs                  Block = 573
	BlockPoplarStairs                    Block = 574
	BlockBambooStairs                    Block = 575
	BlockBambooMosaicStairs              Block = 576
	BlockSlimeBlock                      Block = 577
	BlockBarrier                         Block = 578
	BlockLight                           Block = 579
	BlockIronTrapdoor                    Block = 580
	BlockPrismarine                      Block = 581
	BlockPrismarineBricks                Block = 582
	BlockDarkPrismarine                  Block = 583
	BlockPrismarineStairs                Block = 584
	BlockPrismarineBrickStairs           Block = 585
	BlockDarkPrismarineStairs            Block = 586
	BlockPrismarineSlab                  Block = 587
	BlockPrismarineBrickSlab             Block = 588
	BlockDarkPrismarineSlab              Block = 589
	BlockSeaLantern                      Block = 590
	BlockHayBlock                        Block = 591
	BlockWhiteCarpet                     Block = 592
	BlockOrangeCarpet                    Block = 593
	BlockMagentaCarpet                   Block = 594
	BlockLightBlueCarpet                 Block = 595
	BlockYellowCarpet                    Block = 596
	BlockLimeCarpet                      Block = 597
	BlockPinkCarpet                      Block = 598
	BlockGrayCarpet                      Block = 599
	BlockLightGrayCarpet                 Block = 600
	BlockCyanCarpet                      Block = 601
	BlockPurpleCarpet                    Block = 602
	BlockBlueCarpet                      Block = 603
	BlockBrownCarpet                     Block = 604
	BlockGreenCarpet                     Block = 605
	BlockRedCarpet                       Block = 606
	BlockBlackCarpet                     Block = 607
	BlockTerracotta                      Block = 608
	BlockCoalBlock                       Block = 609
	BlockPackedIce                       Block = 610
	BlockSunflower                       Block = 611
	BlockLilac                           Block = 612
	BlockRoseBush                        Block = 613
	BlockPeony                           Block = 614
	BlockTallGrass                       Block = 615
	BlockLargeFern                       Block = 616
	BlockWhiteBanner                     Block = 617
	BlockOrangeBanner                    Block = 618
	BlockMagentaBanner                   Block = 619
	BlockLightBlueBanner                 Block = 620
	BlockYellowBanner                    Block = 621
	BlockLimeBanner                      Block = 622
	BlockPinkBanner                      Block = 623
	BlockGrayBanner                      Block = 624
	BlockLightGrayBanner                 Block = 625
	BlockCyanBanner                      Block = 626
	BlockPurpleBanner                    Block = 627
	BlockBlueBanner                      Block = 628
	BlockBrownBanner                     Block = 629
	BlockGreenBanner                     Block = 630
	BlockRedBanner                       Block = 631
	BlockBlackBanner                     Block = 632
	BlockWhiteWallBanner                 Block = 633
	BlockOrangeWallBanner                Block = 634
	BlockMagentaWallBanner               Block = 635
	BlockLightBlueWallBanner             Block = 636
	BlockYellowWallBanner                Block = 637
	BlockLimeWallBanner                  Block = 638
	BlockPinkWallBanner                  Block = 639
	BlockGrayWallBanner                  Block = 640
	BlockLightGrayWallBanner             Block = 641
	BlockCyanWallBanner                  Block = 642
	BlockPurpleWallBanner                Block = 643
	BlockBlueWallBanner                  Block = 644
	BlockBrownWallBanner                 Block = 645
	BlockGreenWallBanner                 Block = 646
	BlockRedWallBanner                   Block = 647
	BlockBlackWallBanner                 Block = 648
	BlockRedSandstone                    Block = 649
	BlockChiseledRedSandstone            Block = 650
	BlockCutRedSandstone                 Block = 651
	BlockRedSandstoneStairs              Block = 652
	BlockOakSlab                         Block = 653
	BlockSpruceSlab                      Block = 654
	BlockBirchSlab                       Block = 655
	BlockJungleSlab                      Block = 656
	BlockAcaciaSlab                      Block = 657
	BlockCherrySlab                      Block = 658
	BlockDarkOakSlab                     Block = 659
	BlockPaleOakSlab                     Block = 660
	BlockMangroveSlab                    Block = 661
	BlockPoplarSlab                      Block = 662
	BlockBambooSlab                      Block = 663
	BlockBambooMosaicSlab                Block = 664
	BlockStoneSlab                       Block = 665
	BlockSandstoneSlab                   Block = 666
	BlockCutSandstoneSlab                Block = 667
	BlockPetrifiedOakSlab                Block = 668
	BlockCobblestoneSlab                 Block = 669
	BlockBrickSlab                       Block = 670
	BlockStoneBrickSlab                  Block = 671
	BlockMudBrickSlab                    Block = 672
	BlockNetherBrickSlab                 Block = 673
	BlockQuartzSlab                      Block = 674
	BlockRedSandstoneSlab                Block = 675
	BlockCutRedSandstoneSlab             Block = 676
	BlockSmoothStone                     Block = 677
	BlockSmoothStoneSlab                 Block = 678
	BlockSmoothSandstone                 Block = 679
	BlockSmoothQuartz                    Block = 680
	BlockSmoothRedSandstone              Block = 681
	BlockSpruceFenceGate                 Block = 682
	BlockBirchFenceGate                  Block = 683
	BlockJungleFenceGate                 Block = 684
	BlockAcaciaFenceGate                 Block = 685
	BlockCherryFenceGate                 Block = 686
	BlockDarkOakFenceGate                Block = 687
	BlockPaleOakFenceGate                Block = 688
	BlockMangroveFenceGate               Block = 689
	BlockPoplarFenceGate                 Block = 690
	BlockBambooFenceGate                 Block = 691
	BlockSpruceFence                     Block = 692
	BlockBirchFence                      Block = 693
	BlockJungleFence                     Block = 694
	BlockAcaciaFence                     Block = 695
	BlockCherryFence                     Block = 696
	BlockDarkOakFence                    Block = 697
	BlockPaleOakFence                    Block = 698
	BlockMangroveFence                   Block = 699
	BlockPoplarFence                     Block = 700
	BlockBambooFence                     Block = 701
	BlockSpruceDoor                      Block = 702
	BlockBirchDoor                       Block = 703
	BlockJungleDoor                      Block = 704
	BlockAcaciaDoor                      Block = 705
	BlockCherryDoor                      Block = 706
	BlockDarkOakDoor                     Block = 707
	BlockPaleOakDoor                     Block = 708
	BlockMangroveDoor                    Block = 709
	BlockPoplarDoor                      Block = 710
	BlockBambooDoor                      Block = 711
	BlockEndRod                          Block = 712
	BlockChorusPlant                     Block = 713
	BlockChorusFlower                    Block = 714
	BlockPurpurBlock                     Block = 715
	BlockPurpurSlab                      Block = 716
	BlockPurpurPillar                    Block = 717
	BlockPurpurStairs                    Block = 718
	BlockEndStoneBricks                  Block = 719
	BlockTorchflowerCrop                 Block = 720
	BlockPitcherCrop                     Block = 721
	BlockPitcherPlant                    Block = 722
	BlockBeetroots                       Block = 723
	BlockDirtPath                        Block = 724
	BlockEndGateway                      Block = 725
	BlockRepeatingCommandBlock           Block = 726
	BlockChainCommandBlock               Block = 727
	BlockFrostedIce                      Block = 728
	BlockMagmaBlock                      Block = 729
	BlockNetherWartBlock                 Block = 730
	BlockRedNetherBricks                 Block = 731
	BlockBoneBlock                       Block = 732
	BlockStructureVoid                   Block = 733
	BlockObserver                        Block = 734
	BlockShulkerBox                      Block = 735
	BlockWhiteShulkerBox                 Block = 736
	BlockOrangeShulkerBox                Block = 737
	BlockMagentaShulkerBox               Block = 738
	BlockLightBlueShulkerBox             Block = 739
	BlockYellowShulkerBox                Block = 740
	BlockLimeShulkerBox                  Block = 741
	BlockPinkShulkerBox                  Block = 742
	BlockGrayShulkerBox                  Block = 743
	BlockLightGrayShulkerBox             Block = 744
	BlockCyanShulkerBox                  Block = 745
	BlockPurpleShulkerBox                Block = 746
	BlockBlueShulkerBox                  Block = 747
	BlockBrownShulkerBox                 Block = 748
	BlockGreenShulkerBox                 Block = 749
	BlockRedShulkerBox                   Block = 750
	BlockBlackShulkerBox                 Block = 751
	BlockWhiteGlazedTerracotta           Block = 752
	BlockOrangeGlazedTerracotta          Block = 753
	BlockMagentaGlazedTerracotta         Block = 754
	BlockLightBlueGlazedTerracotta       Block = 755
	BlockYellowGlazedTerracotta          Block = 756
	BlockLimeGlazedTerracotta            Block = 757
	BlockPinkGlazedTerracotta            Block = 758
	BlockGrayGlazedTerracotta            Block = 759
	BlockLightGrayGlazedTerracotta       Block = 760
	BlockCyanGlazedTerracotta            Block = 761
	BlockPurpleGlazedTerracotta          Block = 762
	BlockBlueGlazedTerracotta            Block = 763
	BlockBrownGlazedTerracotta           Block = 764
	BlockGreenGlazedTerracotta           Block = 765
	BlockRedGlazedTerracotta             Block = 766
	BlockBlackGlazedTerracotta           Block = 767
	BlockWhiteConcrete                   Block = 768
	BlockOrangeConcrete                  Block = 769
	BlockMagentaConcrete                 Block = 770
	BlockLightBlueConcrete               Block = 771
	BlockYellowConcrete                  Block = 772
	BlockLimeConcrete                    Block = 773
	BlockPinkConcrete                    Block = 774
	BlockGrayConcrete                    Block = 775
	BlockLightGrayConcrete               Block = 776
	BlockCyanConcrete                    Block = 777
	BlockPurpleConcrete                  Block = 778
	BlockBlueConcrete                    Block = 779
	BlockBrownConcrete                   Block = 780
	BlockGreenConcrete                   Block = 781
	BlockRedConcrete                     Block = 782
	BlockBlackConcrete                   Block = 783
	BlockWhiteConcreteStairs             Block = 784
	BlockOrangeConcreteStairs            Block = 785
	BlockMagentaConcreteStairs           Block = 786
	BlockLightBlueConcreteStairs         Block = 787
	BlockYellowConcreteStairs            Block = 788
	BlockLimeConcreteStairs              Block = 789
	BlockPinkConcreteStairs              Block = 790
	BlockGrayConcreteStairs              Block = 791
	BlockLightGrayConcreteStairs         Block = 792
	BlockCyanConcreteStairs              Block = 793
	BlockPurpleConcreteStairs            Block = 794
	BlockBlueConcreteStairs              Block = 795
	BlockBrownConcreteStairs             Block = 796
	BlockGreenConcreteStairs             Block = 797
	BlockRedConcreteStairs               Block = 798
	BlockBlackConcreteStairs             Block = 799
	BlockWhiteConcreteSlab               Block = 800
	BlockOrangeConcreteSlab              Block = 801
	BlockMagentaConcreteSlab             Block = 802
	BlockLightBlueConcreteSlab           Block = 803
	BlockYellowConcreteSlab              Block = 804
	BlockLimeConcreteSlab                Block = 805
	BlockPinkConcreteSlab                Block = 806
	BlockGrayConcreteSlab                Block = 807
	BlockLightGrayConcreteSlab           Block = 808
	BlockCyanConcreteSlab                Block = 809
	BlockPurpleConcreteSlab              Block = 810
	BlockBlueConcreteSlab                Block = 811
	BlockBrownConcreteSlab               Block = 812
	BlockGreenConcreteSlab               Block = 813
	BlockRedConcreteSlab                 Block = 814
	BlockBlackConcreteSlab               Block = 815
	BlockWhiteConcretePowder             Block = 816
	BlockOrangeConcretePowder            Block = 817
	BlockMagentaConcretePowder           Block = 818
	BlockLightBlueConcretePowder         Block = 819
	BlockYellowConcretePowder            Block = 820
	BlockLimeConcretePowder              Block = 821
	BlockPinkConcretePowder              Block = 822
	BlockGrayConcretePowder              Block = 823
	BlockLightGrayConcretePowder         Block = 824
	BlockCyanConcretePowder              Block = 825
	BlockPurpleConcretePowder            Block = 826
	BlockBlueConcretePowder              Block = 827
	BlockBrownConcretePowder             Block = 828
	BlockGreenConcretePowder             Block = 829
	BlockRedConcretePowder               Block = 830
	BlockBlackConcretePowder             Block = 831
	BlockKelp                            Block = 832
	BlockKelpPlant                       Block = 833
	BlockDriedKelpBlock                  Block = 834
	BlockTurtleEgg                       Block = 835
	BlockSnifferEgg                      Block = 836
	BlockDriedGhast                      Block = 837
	BlockDeadTubeCoralBlock              Block = 838
	BlockDeadBrainCoralBlock             Block = 839
	BlockDeadBubbleCoralBlock            Block = 840
	BlockDeadFireCoralBlock              Block = 841
	BlockDeadHornCoralBlock              Block = 842
	BlockTubeCoralBlock                  Block = 843
	BlockBrainCoralBlock                 Block = 844
	BlockBubbleCoralBlock                Block = 845
	BlockFireCoralBlock                  Block = 846
	BlockHornCoralBlock                  Block = 847
	BlockDeadTubeCoral                   Block = 848
	BlockDeadBrainCoral                  Block = 849
	BlockDeadBubbleCoral                 Block = 850
	BlockDeadFireCoral                   Block = 851
	BlockDeadHornCoral                   Block = 852
	BlockTubeCoral                       Block = 853
	BlockBrainCoral                      Block = 854
	BlockBubbleCoral                     Block = 855
	BlockFireCoral                       Block = 856
	BlockHornCoral                       Block = 857
	BlockDeadTubeCoralFan                Block = 858
	BlockDeadBrainCoralFan               Block = 859
	BlockDeadBubbleCoralFan              Block = 860
	BlockDeadFireCoralFan                Block = 861
	BlockDeadHornCoralFan                Block = 862
	BlockTubeCoralFan                    Block = 863
	BlockBrainCoralFan                   Block = 864
	BlockBubbleCoralFan                  Block = 865
	BlockFireCoralFan                    Block = 866
	BlockHornCoralFan                    Block = 867
	BlockDeadTubeCoralWallFan            Block = 868
	BlockDeadBrainCoralWallFan           Block = 869
	BlockDeadBubbleCoralWallFan          Block = 870
	BlockDeadFireCoralWallFan            Block = 871
	BlockDeadHornCoralWallFan            Block = 872
	BlockTubeCoralWallFan                Block = 873
	BlockBrainCoralWallFan               Block = 874
	BlockBubbleCoralWallFan              Block = 875
	BlockFireCoralWallFan                Block = 876
	BlockHornCoralWallFan                Block = 877
	BlockSeaPickle                       Block = 878
	BlockBlueIce                         Block = 879
	BlockConduit                         Block = 880
	BlockBambooSapling                   Block = 881
	BlockBamboo                          Block = 882
	BlockPottedBamboo                    Block = 883
	BlockVoidAir                         Block = 884
	BlockCaveAir                         Block = 885
	BlockBubbleColumn                    Block = 886
	BlockPolishedGraniteStairs           Block = 887
	BlockSmoothRedSandstoneStairs        Block = 888
	BlockMossyStoneBrickStairs           Block = 889
	BlockPolishedDioriteStairs           Block = 890
	BlockMossyCobblestoneStairs          Block = 891
	BlockEndStoneBrickStairs             Block = 892
	BlockStoneStairs                     Block = 893
	BlockSmoothSandstoneStairs           Block = 894
	BlockSmoothQuartzStairs              Block = 895
	BlockGraniteStairs                   Block = 896
	BlockAndesiteStairs                  Block = 897
	BlockRedNetherBrickStairs            Block = 898
	BlockPolishedAndesiteStairs          Block = 899
	BlockDioriteStairs                   Block = 900
	BlockPolishedGraniteSlab             Block = 901
	BlockSmoothRedSandstoneSlab          Block = 902
	BlockMossyStoneBrickSlab             Block = 903
	BlockPolishedDioriteSlab             Block = 904
	BlockMossyCobblestoneSlab            Block = 905
	BlockEndStoneBrickSlab               Block = 906
	BlockSmoothSandstoneSlab             Block = 907
	BlockSmoothQuartzSlab                Block = 908
	BlockGraniteSlab                     Block = 909
	BlockAndesiteSlab                    Block = 910
	BlockRedNetherBrickSlab              Block = 911
	BlockPolishedAndesiteSlab            Block = 912
	BlockDioriteSlab                     Block = 913
	BlockBrickWall                       Block = 914
	BlockPrismarineWall                  Block = 915
	BlockRedSandstoneWall                Block = 916
	BlockMossyStoneBrickWall             Block = 917
	BlockGraniteWall                     Block = 918
	BlockStoneBrickWall                  Block = 919
	BlockMudBrickWall                    Block = 920
	BlockNetherBrickWall                 Block = 921
	BlockAndesiteWall                    Block = 922
	BlockRedNetherBrickWall              Block = 923
	BlockSandstoneWall                   Block = 924
	BlockEndStoneBrickWall               Block = 925
	BlockDioriteWall                     Block = 926
	BlockScaffolding                     Block = 927
	BlockLoom                            Block = 928
	BlockBarrel                          Block = 929
	BlockSmoker                          Block = 930
	BlockBlastFurnace                    Block = 931
	BlockCartographyTable                Block = 932
	BlockFletchingTable                  Block = 933
	BlockGrindstone                      Block = 934
	BlockLectern                         Block = 935
	BlockSmithingTable                   Block = 936
	BlockStonecutter                     Block = 937
	BlockBell                            Block = 938
	BlockLantern                         Block = 939
	BlockSoulLantern                     Block = 940
	BlockCopperLantern                   Block = 941
	BlockExposedCopperLantern            Block = 942
	BlockWeatheredCopperLantern          Block = 943
	BlockOxidizedCopperLantern           Block = 944
	BlockWaxedCopperLantern              Block = 945
	BlockWaxedExposedCopperLantern       Block = 946
	BlockWaxedWeatheredCopperLantern     Block = 947
	BlockWaxedOxidizedCopperLantern      Block = 948
	BlockCampfire                        Block = 949
	BlockSoulCampfire                    Block = 950
	BlockSweetBerryBush                  Block = 951
	BlockWarpedStem                      Block = 952
	BlockStrippedWarpedStem              Block = 953
	BlockWarpedHyphae                    Block = 954
	BlockStrippedWarpedHyphae            Block = 955
	BlockWarpedNylium                    Block = 956
	BlockWarpedFungus                    Block = 957
	BlockWarpedWartBlock                 Block = 958
	BlockWarpedRoots                     Block = 959
	BlockNetherSprouts                   Block = 960
	BlockCrimsonStem                     Block = 961
	BlockStrippedCrimsonStem             Block = 962
	BlockCrimsonHyphae                   Block = 963
	BlockStrippedCrimsonHyphae           Block = 964
	BlockCrimsonNylium                   Block = 965
	BlockCrimsonFungus                   Block = 966
	BlockShroomlight                     Block = 967
	BlockWeepingVines                    Block = 968
	BlockWeepingVinesPlant               Block = 969
	BlockTwistingVines                   Block = 970
	BlockTwistingVinesPlant              Block = 971
	BlockCrimsonRoots                    Block = 972
	BlockCrimsonPlanks                   Block = 973
	BlockWarpedPlanks                    Block = 974
	BlockCrimsonSlab                     Block = 975
	BlockWarpedSlab                      Block = 976
	BlockCrimsonPressurePlate            Block = 977
	BlockWarpedPressurePlate             Block = 978
	BlockCrimsonFence                    Block = 979
	BlockWarpedFence                     Block = 980
	BlockCrimsonTrapdoor                 Block = 981
	BlockWarpedTrapdoor                  Block = 982
	BlockCrimsonFenceGate                Block = 983
	BlockWarpedFenceGate                 Block = 984
	BlockCrimsonStairs                   Block = 985
	BlockWarpedStairs                    Block = 986
	BlockCrimsonButton                   Block = 987
	BlockWarpedButton                    Block = 988
	BlockCrimsonDoor                     Block = 989
	BlockWarpedDoor                      Block = 990
	BlockCrimsonSign                     Block = 991
	BlockWarpedSign                      Block = 992
	BlockCrimsonWallSign                 Block = 993
	BlockWarpedWallSign                  Block = 994
	BlockStructureBlock                  Block = 995
	BlockJigsaw                          Block = 996
	BlockTestBlock                       Block = 997
	BlockTestInstanceBlock               Block = 998
	BlockComposter                       Block = 999
	BlockTarget                          Block = 1000
	BlockBeeNest                         Block = 1001
	BlockBeehive                         Block = 1002
	BlockHoneyBlock                      Block = 1003
	BlockHoneycombBlock                  Block = 1004
	BlockNetheriteBlock                  Block = 1005
	BlockAncientDebris                   Block = 1006
	BlockCryingObsidian                  Block = 1007
	BlockRespawnAnchor                   Block = 1008
	BlockPottedCrimsonFungus             Block = 1009
	BlockPottedWarpedFungus              Block = 1010
	BlockPottedCrimsonRoots              Block = 1011
	BlockPottedWarpedRoots               Block = 1012
	BlockLodestone                       Block = 1013
	BlockBlackstone                      Block = 1014
	BlockBlackstoneStairs                Block = 1015
	BlockBlackstoneWall                  Block = 1016
	BlockBlackstoneSlab                  Block = 1017
	BlockPolishedBlackstone              Block = 1018
	BlockPolishedBlackstoneBricks        Block = 1019
	BlockCrackedPolishedBlackstoneBricks Block = 1020
	BlockChiseledPolishedBlackstone      Block = 1021
	BlockPolishedBlackstoneBrickSlab     Block = 1022
	BlockPolishedBlackstoneBrickStairs   Block = 1023
	BlockPolishedBlackstoneBrickWall     Block = 1024
	BlockGildedBlackstone                Block = 1025
	BlockPolishedBlackstoneStairs        Block = 1026
	BlockPolishedBlackstoneSlab          Block = 1027
	BlockPolishedBlackstonePressurePlate Block = 1028
	BlockPolishedBlackstoneButton        Block = 1029
	BlockPolishedBlackstoneWall          Block = 1030
	BlockChiseledNetherBricks            Block = 1031
	BlockCrackedNetherBricks             Block = 1032
	BlockQuartzBricks                    Block = 1033
	BlockCandle                          Block = 1034
	BlockWhiteCandle                     Block = 1035
	BlockOrangeCandle                    Block = 1036
	BlockMagentaCandle                   Block = 1037
	BlockLightBlueCandle                 Block = 1038
	BlockYellowCandle                    Block = 1039
	BlockLimeCandle                      Block = 1040
	BlockPinkCandle                      Block = 1041
	BlockGrayCandle                      Block = 1042
	BlockLightGrayCandle                 Block = 1043
	BlockCyanCandle                      Block = 1044
	BlockPurpleCandle                    Block = 1045
	BlockBlueCandle                      Block = 1046
	BlockBrownCandle                     Block = 1047
	BlockGreenCandle                     Block = 1048
	BlockRedCandle                       Block = 1049
	BlockBlackCandle                     Block = 1050
	BlockCandleCake                      Block = 1051
	BlockWhiteCandleCake                 Block = 1052
	BlockOrangeCandleCake                Block = 1053
	BlockMagentaCandleCake               Block = 1054
	BlockLightBlueCandleCake             Block = 1055
	BlockYellowCandleCake                Block = 1056
	BlockLimeCandleCake                  Block = 1057
	BlockPinkCandleCake                  Block = 1058
	BlockGrayCandleCake                  Block = 1059
	BlockLightGrayCandleCake             Block = 1060
	BlockCyanCandleCake                  Block = 1061
	BlockPurpleCandleCake                Block = 1062
	BlockBlueCandleCake                  Block = 1063
	BlockBrownCandleCake                 Block = 1064
	BlockGreenCandleCake                 Block = 1065
	BlockRedCandleCake                   Block = 1066
	BlockBlackCandleCake                 Block = 1067
	BlockAmethystBlock                   Block = 1068
	BlockBuddingAmethyst                 Block = 1069
	BlockAmethystCluster                 Block = 1070
	BlockLargeAmethystBud                Block = 1071
	BlockMediumAmethystBud               Block = 1072
	BlockSmallAmethystBud                Block = 1073
	BlockTuff                            Block = 1074
	BlockTuffSlab                        Block = 1075
	BlockTuffStairs                      Block = 1076
	BlockTuffWall                        Block = 1077
	BlockPolishedTuff                    Block = 1078
	BlockPolishedTuffSlab                Block = 1079
	BlockPolishedTuffStairs              Block = 1080
	BlockPolishedTuffWall                Block = 1081
	BlockChiseledTuff                    Block = 1082
	BlockTuffBricks                      Block = 1083
	BlockTuffBrickSlab                   Block = 1084
	BlockTuffBrickStairs                 Block = 1085
	BlockTuffBrickWall                   Block = 1086
	BlockChiseledTuffBricks              Block = 1087
	BlockSulfur                          Block = 1088
	BlockPotentSulfur                    Block = 1089
	BlockSulfurSlab                      Block = 1090
	BlockSulfurStairs                    Block = 1091
	BlockSulfurWall                      Block = 1092
	BlockPolishedSulfur                  Block = 1093
	BlockPolishedSulfurSlab              Block = 1094
	BlockPolishedSulfurStairs            Block = 1095
	BlockPolishedSulfurWall              Block = 1096
	BlockSulfurBricks                    Block = 1097
	BlockSulfurBrickSlab                 Block = 1098
	BlockSulfurBrickStairs               Block = 1099
	BlockSulfurBrickWall                 Block = 1100
	BlockChiseledSulfur                  Block = 1101
	BlockCinnabar                        Block = 1102
	BlockCinnabarSlab                    Block = 1103
	BlockCinnabarStairs                  Block = 1104
	BlockCinnabarWall                    Block = 1105
	BlockPolishedCinnabar                Block = 1106
	BlockPolishedCinnabarSlab            Block = 1107
	BlockPolishedCinnabarStairs          Block = 1108
	BlockPolishedCinnabarWall            Block = 1109
	BlockCinnabarBricks                  Block = 1110
	BlockCinnabarBrickSlab               Block = 1111
	BlockCinnabarBrickStairs             Block = 1112
	BlockCinnabarBrickWall               Block = 1113
	BlockChiseledCinnabar                Block = 1114
	BlockCalcite                         Block = 1115
	BlockTintedGlass                     Block = 1116
	BlockPowderSnow                      Block = 1117
	BlockSculkSensor                     Block = 1118
	BlockCalibratedSculkSensor           Block = 1119
	BlockSculk                           Block = 1120
	BlockSculkVein                       Block = 1121
	BlockSculkCatalyst                   Block = 1122
	BlockSculkShrieker                   Block = 1123
	BlockCopperBlock                     Block = 1124
	BlockExposedCopper                   Block = 1125
	BlockWeatheredCopper                 Block = 1126
	BlockOxidizedCopper                  Block = 1127
	BlockWaxedCopperBlock                Block = 1128
	BlockWaxedExposedCopper              Block = 1129
	BlockWaxedWeatheredCopper            Block = 1130
	BlockWaxedOxidizedCopper             Block = 1131
	BlockCopperOre                       Block = 1132
	BlockDeepslateCopperOre              Block = 1133
	BlockCutCopper                       Block = 1134
	BlockExposedCutCopper                Block = 1135
	BlockWeatheredCutCopper              Block = 1136
	BlockOxidizedCutCopper               Block = 1137
	BlockWaxedCutCopper                  Block = 1138
	BlockWaxedExposedCutCopper           Block = 1139
	BlockWaxedWeatheredCutCopper         Block = 1140
	BlockWaxedOxidizedCutCopper          Block = 1141
	BlockChiseledCopper                  Block = 1142
	BlockExposedChiseledCopper           Block = 1143
	BlockWeatheredChiseledCopper         Block = 1144
	BlockOxidizedChiseledCopper          Block = 1145
	BlockWaxedChiseledCopper             Block = 1146
	BlockWaxedExposedChiseledCopper      Block = 1147
	BlockWaxedWeatheredChiseledCopper    Block = 1148
	BlockWaxedOxidizedChiseledCopper     Block = 1149
	BlockCutCopperStairs                 Block = 1150
	BlockExposedCutCopperStairs          Block = 1151
	BlockWeatheredCutCopperStairs        Block = 1152
	BlockOxidizedCutCopperStairs         Block = 1153
	BlockWaxedCutCopperStairs            Block = 1154
	BlockWaxedExposedCutCopperStairs     Block = 1155
	BlockWaxedWeatheredCutCopperStairs   Block = 1156
	BlockWaxedOxidizedCutCopperStairs    Block = 1157
	BlockCutCopperSlab                   Block = 1158
	BlockExposedCutCopperSlab            Block = 1159
	BlockWeatheredCutCopperSlab          Block = 1160
	BlockOxidizedCutCopperSlab           Block = 1161
	BlockWaxedCutCopperSlab              Block = 1162
	BlockWaxedExposedCutCopperSlab       Block = 1163
	BlockWaxedWeatheredCutCopperSlab     Block = 1164
	BlockWaxedOxidizedCutCopperSlab      Block = 1165
	BlockCopperDoor                      Block = 1166
	BlockExposedCopperDoor               Block = 1167
	BlockWeatheredCopperDoor             Block = 1168
	BlockOxidizedCopperDoor              Block = 1169
	BlockWaxedCopperDoor                 Block = 1170
	BlockWaxedExposedCopperDoor          Block = 1171
	BlockWaxedWeatheredCopperDoor        Block = 1172
	BlockWaxedOxidizedCopperDoor         Block = 1173
	BlockCopperTrapdoor                  Block = 1174
	BlockExposedCopperTrapdoor           Block = 1175
	BlockWeatheredCopperTrapdoor         Block = 1176
	BlockOxidizedCopperTrapdoor          Block = 1177
	BlockWaxedCopperTrapdoor             Block = 1178
	BlockWaxedExposedCopperTrapdoor      Block = 1179
	BlockWaxedWeatheredCopperTrapdoor    Block = 1180
	BlockWaxedOxidizedCopperTrapdoor     Block = 1181
	BlockCopperGrate                     Block = 1182
	BlockExposedCopperGrate              Block = 1183
	BlockWeatheredCopperGrate            Block = 1184
	BlockOxidizedCopperGrate             Block = 1185
	BlockWaxedCopperGrate                Block = 1186
	BlockWaxedExposedCopperGrate         Block = 1187
	BlockWaxedWeatheredCopperGrate       Block = 1188
	BlockWaxedOxidizedCopperGrate        Block = 1189
	BlockCopperBulb                      Block = 1190
	BlockExposedCopperBulb               Block = 1191
	BlockWeatheredCopperBulb             Block = 1192
	BlockOxidizedCopperBulb              Block = 1193
	BlockWaxedCopperBulb                 Block = 1194
	BlockWaxedExposedCopperBulb          Block = 1195
	BlockWaxedWeatheredCopperBulb        Block = 1196
	BlockWaxedOxidizedCopperBulb         Block = 1197
	BlockCopperChest                     Block = 1198
	BlockExposedCopperChest              Block = 1199
	BlockWeatheredCopperChest            Block = 1200
	BlockOxidizedCopperChest             Block = 1201
	BlockWaxedCopperChest                Block = 1202
	BlockWaxedExposedCopperChest         Block = 1203
	BlockWaxedWeatheredCopperChest       Block = 1204
	BlockWaxedOxidizedCopperChest        Block = 1205
	BlockCopperGolemStatue               Block = 1206
	BlockExposedCopperGolemStatue        Block = 1207
	BlockWeatheredCopperGolemStatue      Block = 1208
	BlockOxidizedCopperGolemStatue       Block = 1209
	BlockWaxedCopperGolemStatue          Block = 1210
	BlockWaxedExposedCopperGolemStatue   Block = 1211
	BlockWaxedWeatheredCopperGolemStatue Block = 1212
	BlockWaxedOxidizedCopperGolemStatue  Block = 1213
	BlockLightningRod                    Block = 1214
	BlockExposedLightningRod             Block = 1215
	BlockWeatheredLightningRod           Block = 1216
	BlockOxidizedLightningRod            Block = 1217
	BlockWaxedLightningRod               Block = 1218
	BlockWaxedExposedLightningRod        Block = 1219
	BlockWaxedWeatheredLightningRod      Block = 1220
	BlockWaxedOxidizedLightningRod       Block = 1221
	BlockDripstoneBlock                  Block = 1222
	BlockPointedDripstone                Block = 1223
	BlockSulfurSpike                     Block = 1224
	BlockCaveVines                       Block = 1225
	BlockCaveVinesPlant                  Block = 1226
	BlockSporeBlossom                    Block = 1227
	BlockAzalea                          Block = 1228
	BlockFloweringAzalea                 Block = 1229
	BlockMossCarpet                      Block = 1230
	BlockPinkPetals                      Block = 1231
	BlockWildflowers                     Block = 1232
	BlockLeafLitter                      Block = 1233
	BlockMossBlock                       Block = 1234
	BlockBigDripleaf                     Block = 1235
	BlockBigDripleafStem                 Block = 1236
	BlockSmallDripleaf                   Block = 1237
	BlockHangingRoots                    Block = 1238
	BlockRootedDirt                      Block = 1239
	BlockMud                             Block = 1240
	BlockDeepslate                       Block = 1241
	BlockCobbledDeepslate                Block = 1242
	BlockCobbledDeepslateStairs          Block = 1243
	BlockCobbledDeepslateSlab            Block = 1244
	BlockCobbledDeepslateWall            Block = 1245
	BlockPolishedDeepslate               Block = 1246
	BlockPolishedDeepslateStairs         Block = 1247
	BlockPolishedDeepslateSlab           Block = 1248
	BlockPolishedDeepslateWall           Block = 1249
	BlockDeepslateTiles                  Block = 1250
	BlockDeepslateTileStairs             Block = 1251
	BlockDeepslateTileSlab               Block = 1252
	BlockDeepslateTileWall               Block = 1253
	BlockDeepslateBricks                 Block = 1254
	BlockDeepslateBrickStairs            Block = 1255
	BlockDeepslateBrickSlab              Block = 1256
	BlockDeepslateBrickWall              Block = 1257
	BlockChiseledDeepslate               Block = 1258
	BlockCrackedDeepslateBricks          Block = 1259
	BlockCrackedDeepslateTiles           Block = 1260
	BlockInfestedDeepslate               Block = 1261
	BlockSmoothBasalt                    Block = 1262
	BlockRawIronBlock                    Block = 1263
	BlockRawCopperBlock                  Block = 1264
	BlockRawGoldBlock                    Block = 1265
	BlockPottedAzaleaBush                Block = 1266
	BlockPottedFloweringAzaleaBush       Block = 1267
	BlockOchreFroglight                  Block = 1268
	BlockVerdantFroglight                Block = 1269
	BlockPearlescentFroglight            Block = 1270
	BlockFrogspawn                       Block = 1271
	BlockReinforcedDeepslate             Block = 1272
	BlockDecoratedPot                    Block = 1273
	BlockCrafter                         Block = 1274
	BlockTrialSpawner                    Block = 1275
	BlockVault                           Block = 1276
	BlockHeavyCore                       Block = 1277
	BlockPaleMossBlock                   Block = 1278
	BlockPaleMossCarpet                  Block = 1279
	BlockPaleHangingMoss                 Block = 1280
	BlockOpenEyeblossom                  Block = 1281
	BlockClosedEyeblossom                Block = 1282
	BlockPottedOpenEyeblossom            Block = 1283
	BlockPottedClosedEyeblossom          Block = 1284
	BlockFireflyBush                     Block = 1285
)

// blockInfo is each block's name, first state ID and default state ID, by
// Block. A block's states run from its first to the next block's first.
var blockInfo = [BlockCount]struct {
	name         string
	first, deflt uint16
}{
	{"minecraft:air", 0, 0},
	{"minecraft:stone", 1, 1},
	{"minecraft:granite", 2, 2},
	{"minecraft:polished_granite", 3, 3},
	{"minecraft:diorite", 4, 4},
	{"minecraft:polished_diorite", 5, 5},
	{"minecraft:andesite", 6, 6},
	{"minecraft:polished_andesite", 7, 7},
	{"minecraft:grass_block", 8, 9},
	{"minecraft:dirt", 10, 10},
	{"minecraft:coarse_dirt", 11, 11},
	{"minecraft:podzol", 12, 13},
	{"minecraft:cobblestone", 14, 14},
	{"minecraft:oak_planks", 15, 15},
	{"minecraft:spruce_planks", 16, 16},
	{"minecraft:birch_planks", 17, 17},
	{"minecraft:jungle_planks", 18, 18},
	{"minecraft:acacia_planks", 19, 19},
	{"minecraft:cherry_planks", 20, 20},
	{"minecraft:dark_oak_planks", 21, 21},
	{"minecraft:pale_oak_wood", 22, 23},
	{"minecraft:pale_oak_planks", 25, 25},
	{"minecraft:mangrove_planks", 26, 26},
	{"minecraft:poplar_planks", 27, 27},
	{"minecraft:bamboo_planks", 28, 28},
	{"minecraft:bamboo_mosaic", 29, 29},
	{"minecraft:oak_sapling", 30, 30},
	{"minecraft:spruce_sapling", 32, 32},
	{"minecraft:birch_sapling", 34, 34},
	{"minecraft:jungle_sapling", 36, 36},
	{"minecraft:acacia_sapling", 38, 38},
	{"minecraft:cherry_sapling", 40, 40},
	{"minecraft:dark_oak_sapling", 42, 42},
	{"minecraft:pale_oak_sapling", 44, 44},
	{"minecraft:mangrove_propagule", 46, 51},
	{"minecraft:poplar_sapling", 86, 86},
	{"minecraft:bedrock", 88, 88},
	{"minecraft:water", 89, 89},
	{"minecraft:lava", 105, 105},
	{"minecraft:sand", 121, 121},
	{"minecraft:suspicious_sand", 122, 122},
	{"minecraft:red_sand", 126, 126},
	{"minecraft:gravel", 127, 127},
	{"minecraft:suspicious_gravel", 128, 128},
	{"minecraft:gold_ore", 132, 132},
	{"minecraft:deepslate_gold_ore", 133, 133},
	{"minecraft:iron_ore", 134, 134},
	{"minecraft:deepslate_iron_ore", 135, 135},
	{"minecraft:coal_ore", 136, 136},
	{"minecraft:deepslate_coal_ore", 137, 137},
	{"minecraft:nether_gold_ore", 138, 138},
	{"minecraft:oak_log", 139, 140},
	{"minecraft:spruce_log", 142, 143},
	{"minecraft:birch_log", 145, 146},
	{"minecraft:jungle_log", 148, 149},
	{"minecraft:acacia_log", 151, 152},
	{"minecraft:cherry_log", 154, 155},
	{"minecraft:dark_oak_log", 157, 158},
	{"minecraft:pale_oak_log", 160, 161},
	{"minecraft:mangrove_log", 163, 164},
	{"minecraft:poplar_log", 166, 167},
	{"minecraft:mangrove_roots", 169, 170},
	{"minecraft:muddy_mangrove_roots", 171, 172},
	{"minecraft:bamboo_block", 174, 175},
	{"minecraft:stripped_spruce_log", 177, 178},
	{"minecraft:stripped_birch_log", 180, 181},
	{"minecraft:stripped_jungle_log", 183, 184},
	{"minecraft:stripped_acacia_log", 186, 187},
	{"minecraft:stripped_cherry_log", 189, 190},
	{"minecraft:stripped_dark_oak_log", 192, 193},
	{"minecraft:stripped_pale_oak_log", 195, 196},
	{"minecraft:stripped_oak_log", 198, 199},
	{"minecraft:stripped_mangrove_log", 201, 202},
	{"minecraft:stripped_poplar_log", 204, 205},
	{"minecraft:stripped_bamboo_block", 207, 208},
	{"minecraft:oak_wood", 210, 211},
	{"minecraft:spruce_wood", 213, 214},
	{"minecraft:birch_wood", 216, 217},
	{"minecraft:jungle_wood", 219, 220},
	{"minecraft:acacia_wood", 222, 223},
	{"minecraft:cherry_wood", 225, 226},
	{"minecraft:dark_oak_wood", 228, 229},
	{"minecraft:mangrove_wood", 231, 232},
	{"minecraft:poplar_wood", 234, 235},
	{"minecraft:stripped_oak_wood", 237, 238},
	{"minecraft:stripped_spruce_wood", 240, 241},
	{"minecraft:stripped_birch_wood", 243, 244},
	{"minecraft:stripped_jungle_wood", 246, 247},
	{"minecraft:stripped_acacia_wood", 249, 250},
	{"minecraft:stripped_cherry_wood", 252, 253},
	{"minecraft:stripped_dark_oak_wood", 255, 256},
	{"minecraft:stripped_pale_oak_wood", 258, 259},
	{"minecraft:stripped_mangrove_wood", 261, 262},
	{"minecraft:stripped_poplar_wood", 264, 265},
	{"minecraft:oak_leaves", 267, 294},
	{"minecraft:spruce_leaves", 295, 322},
	{"minecraft:birch_leaves", 323, 350},
	{"minecraft:jungle_leaves", 351, 378},
	{"minecraft:acacia_leaves", 379, 406},
	{"minecraft:cherry_leaves", 407, 434},
	{"minecraft:dark_oak_leaves", 435, 462},
	{"minecraft:pale_oak_leaves", 463, 490},
	{"minecraft:mangrove_leaves", 491, 518},
	{"minecraft:red_poplar_leaves", 519, 546},
	{"minecraft:orange_poplar_leaves", 547, 574},
	{"minecraft:yellow_poplar_leaves", 575, 602},
	{"minecraft:azalea_leaves", 603, 630},
	{"minecraft:flowering_azalea_leaves", 631, 658},
	{"minecraft:sponge", 659, 659},
	{"minecraft:wet_sponge", 660, 660},
	{"minecraft:glass", 661, 661},
	{"minecraft:lapis_ore", 662, 662},
	{"minecraft:deepslate_lapis_ore", 663, 663},
	{"minecraft:lapis_block", 664, 664},
	{"minecraft:dispenser", 665, 666},
	{"minecraft:sandstone", 677, 677},
	{"minecraft:chiseled_sandstone", 678, 678},
	{"minecraft:cut_sandstone", 679, 679},
	{"minecraft:note_block", 680, 681},
	{"minecraft:white_bed", 2030, 2033},
	{"minecraft:orange_bed", 2046, 2049},
	{"minecraft:magenta_bed", 2062, 2065},
	{"minecraft:light_blue_bed", 2078, 2081},
	{"minecraft:yellow_bed", 2094, 2097},
	{"minecraft:lime_bed", 2110, 2113},
	{"minecraft:pink_bed", 2126, 2129},
	{"minecraft:gray_bed", 2142, 2145},
	{"minecraft:light_gray_bed", 2158, 2161},
	{"minecraft:cyan_bed", 2174, 2177},
	{"minecraft:purple_bed", 2190, 2193},
	{"minecraft:blue_bed", 2206, 2209},
	{"minecraft:brown_bed", 2222, 2225},
	{"minecraft:green_bed", 2238, 2241},
	{"minecraft:red_bed", 2254, 2257},
	{"minecraft:black_bed", 2270, 2273},
	{"minecraft:straw_bed", 2286, 2289},
	{"minecraft:powered_rail", 2302, 2315},
	{"minecraft:detector_rail", 2326, 2339},
	{"minecraft:sticky_piston", 2350, 2356},
	{"minecraft:cobweb", 2362, 2362},
	{"minecraft:short_grass", 2363, 2363},
	{"minecraft:fern", 2364, 2364},
	{"minecraft:dead_bush", 2365, 2365},
	{"minecraft:bush", 2366, 2366},
	{"minecraft:red_shrub", 2367, 2367},
	{"minecraft:short_dry_grass", 2368, 2368},
	{"minecraft:tall_dry_grass", 2369, 2369},
	{"minecraft:seagrass", 2370, 2370},
	{"minecraft:tall_seagrass", 2371, 2372},
	{"minecraft:piston", 2373, 2379},
	{"minecraft:piston_head", 2385, 2387},
	{"minecraft:white_wool", 2409, 2409},
	{"minecraft:orange_wool", 2410, 2410},
	{"minecraft:magenta_wool", 2411, 2411},
	{"minecraft:light_blue_wool", 2412, 2412},
	{"minecraft:yellow_wool", 2413, 2413},
	{"minecraft:lime_wool", 2414, 2414},
	{"minecraft:pink_wool", 2415, 2415},
	{"minecraft:gray_wool", 2416, 2416},
	{"minecraft:light_gray_wool", 2417, 2417},
	{"minecraft:cyan_wool", 2418, 2418},
	{"minecraft:purple_wool", 2419, 2419},
	{"minecraft:blue_wool", 2420, 2420},
	{"minecraft:brown_wool", 2421, 2421},
	{"minecraft:green_wool", 2422, 2422},
	{"minecraft:red_wool", 2423, 2423},
	{"minecraft:black_wool", 2424, 2424},
	{"minecraft:white_wool_stairs", 2425, 2436},
	{"minecraft:orange_wool_stairs", 2505, 2516},
	{"minecraft:magenta_wool_stairs", 2585, 2596},
	{"minecraft:light_blue_wool_stairs", 2665, 2676},
	{"minecraft:yellow_wool_stairs", 2745, 2756},
	{"minecraft:lime_wool_stairs", 2825, 2836},
	{"minecraft:pink_wool_stairs", 2905, 2916},
	{"minecraft:gray_wool_stairs", 2985, 2996},
	{"minecraft:light_gray_wool_stairs", 3065, 3076},
	{"minecraft:cyan_wool_stairs", 3145, 3156},
	{"minecraft:purple_wool_stairs", 3225, 3236},
	{"minecraft:blue_wool_stairs", 3305, 3316},
	{"minecraft:brown_wool_stairs", 3385, 3396},
	{"minecraft:green_wool_stairs", 3465, 3476},
	{"minecraft:red_wool_stairs", 3545, 3556},
	{"minecraft:black_wool_stairs", 3625, 3636},
	{"minecraft:white_wool_slab", 3705, 3708},
	{"minecraft:orange_wool_slab", 3711, 3714},
	{"minecraft:magenta_wool_slab", 3717, 3720},
	{"minecraft:light_blue_wool_slab", 3723, 3726},
	{"minecraft:yellow_wool_slab", 3729, 3732},
	{"minecraft:lime_wool_slab", 3735, 3738},
	{"minecraft:pink_wool_slab", 3741, 3744},
	{"minecraft:gray_wool_slab", 3747, 3750},
	{"minecraft:light_gray_wool_slab", 3753, 3756},
	{"minecraft:cyan_wool_slab", 3759, 3762},
	{"minecraft:purple_wool_slab", 3765, 3768},
	{"minecraft:blue_wool_slab", 3771, 3774},
	{"minecraft:brown_wool_slab", 3777, 3780},
	{"minecraft:green_wool_slab", 3783, 3786},
	{"minecraft:red_wool_slab", 3789, 3792},
	{"minecraft:black_wool_slab", 3795, 3798},
	{"minecraft:moving_piston", 3801, 3801},
	{"minecraft:dandelion", 3813, 3813},
	{"minecraft:golden_dandelion", 3814, 3814},
	{"minecraft:torchflower", 3815, 3815},
	{"minecraft:poppy", 3816, 3816},
	{"minecraft:blue_orchid", 3817, 3817},
	{"minecraft:allium", 3818, 3818},
	{"minecraft:azure_bluet", 3819, 3819},
	{"minecraft:red_tulip", 3820, 3820},
	{"minecraft:orange_tulip", 3821, 3821},
	{"minecraft:white_tulip", 3822, 3822},
	{"minecraft:pink_tulip", 3823, 3823},
	{"minecraft:oxeye_daisy", 3824, 3824},
	{"minecraft:cornflower", 3825, 3825},
	{"minecraft:wither_rose", 3826, 3826},
	{"minecraft:lily_of_the_valley", 3827, 3827},
	{"minecraft:brown_mushroom", 3828, 3828},
	{"minecraft:red_mushroom", 3829, 3829},
	{"minecraft:gold_block", 3830, 3830},
	{"minecraft:iron_block", 3831, 3831},
	{"minecraft:bricks", 3832, 3832},
	{"minecraft:tnt", 3833, 3834},
	{"minecraft:bookshelf", 3835, 3835},
	{"minecraft:chiseled_bookshelf", 3836, 3899},
	{"minecraft:acacia_shelf", 4092, 4101},
	{"minecraft:bamboo_shelf", 4156, 4165},
	{"minecraft:birch_shelf", 4220, 4229},
	{"minecraft:cherry_shelf", 4284, 4293},
	{"minecraft:crimson_shelf", 4348, 4357},
	{"minecraft:dark_oak_shelf", 4412, 4421},
	{"minecraft:jungle_shelf", 4476, 4485},
	{"minecraft:mangrove_shelf", 4540, 4549},
	{"minecraft:oak_shelf", 4604, 4613},
	{"minecraft:pale_oak_shelf", 4668, 4677},
	{"minecraft:poplar_shelf", 4732, 4741},
	{"minecraft:spruce_shelf", 4796, 4805},
	{"minecraft:warped_shelf", 4860, 4869},
	{"minecraft:mossy_cobblestone", 4924, 4924},
	{"minecraft:obsidian", 4925, 4925},
	{"minecraft:torch", 4926, 4926},
	{"minecraft:wall_torch", 4927, 4927},
	{"minecraft:fire", 4931, 4962},
	{"minecraft:soul_fire", 5443, 5443},
	{"minecraft:spawner", 5444, 5444},
	{"minecraft:creaking_heart", 5445, 5452},
	{"minecraft:oak_stairs", 5463, 5474},
	{"minecraft:chest", 5543, 5544},
	{"minecraft:redstone_wire", 5567, 6727},
	{"minecraft:diamond_ore", 6863, 6863},
	{"minecraft:deepslate_diamond_ore", 6864, 6864},
	{"minecraft:diamond_block", 6865, 6865},
	{"minecraft:crafting_table", 6866, 6866},
	{"minecraft:wheat", 6867, 6867},
	{"minecraft:farmland", 6875, 6875},
	{"minecraft:furnace", 6883, 6884},
	{"minecraft:oak_sign", 6891, 6908},
	{"minecraft:spruce_sign", 6923, 6940},
	{"minecraft:birch_sign", 6955, 6972},
	{"minecraft:acacia_sign", 6987, 7004},
	{"minecraft:cherry_sign", 7019, 7036},
	{"minecraft:jungle_sign", 7051, 7068},
	{"minecraft:dark_oak_sign", 7083, 7100},
	{"minecraft:pale_oak_sign", 7115, 7132},
	{"minecraft:mangrove_sign", 7147, 7164},
	{"minecraft:poplar_sign", 7179, 7196},
	{"minecraft:bamboo_sign", 7211, 7228},
	{"minecraft:oak_door", 7243, 7254},
	{"minecraft:ladder", 7307, 7308},
	{"minecraft:rail", 7315, 7316},
	{"minecraft:cobblestone_stairs", 7335, 7346},
	{"minecraft:oak_wall_sign", 7415, 7416},
	{"minecraft:spruce_wall_sign", 7423, 7424},
	{"minecraft:birch_wall_sign", 7431, 7432},
	{"minecraft:acacia_wall_sign", 7439, 7440},
	{"minecraft:cherry_wall_sign", 7447, 7448},
	{"minecraft:jungle_wall_sign", 7455, 7456},
	{"minecraft:dark_oak_wall_sign", 7463, 7464},
	{"minecraft:pale_oak_wall_sign", 7471, 7472},
	{"minecraft:mangrove_wall_sign", 7479, 7480},
	{"minecraft:poplar_wall_sign", 7487, 7488},
	{"minecraft:bamboo_wall_sign", 7495, 7496},
	{"minecraft:oak_hanging_sign", 7503, 7552},
	{"minecraft:spruce_hanging_sign", 7567, 7616},
	{"minecraft:birch_hanging_sign", 7631, 7680},
	{"minecraft:acacia_hanging_sign", 7695, 7744},
	{"minecraft:cherry_hanging_sign", 7759, 7808},
	{"minecraft:jungle_hanging_sign", 7823, 7872},
	{"minecraft:dark_oak_hanging_sign", 7887, 7936},
	{"minecraft:pale_oak_hanging_sign", 7951, 8000},
	{"minecraft:crimson_hanging_sign", 8015, 8064},
	{"minecraft:warped_hanging_sign", 8079, 8128},
	{"minecraft:mangrove_hanging_sign", 8143, 8192},
	{"minecraft:poplar_hanging_sign", 8207, 8256},
	{"minecraft:bamboo_hanging_sign", 8271, 8320},
	{"minecraft:oak_wall_hanging_sign", 8335, 8336},
	{"minecraft:spruce_wall_hanging_sign", 8343, 8344},
	{"minecraft:birch_wall_hanging_sign", 8351, 8352},
	{"minecraft:acacia_wall_hanging_sign", 8359, 8360},
	{"minecraft:cherry_wall_hanging_sign", 8367, 8368},
	{"minecraft:jungle_wall_hanging_sign", 8375, 8376},
	{"minecraft:dark_oak_wall_hanging_sign", 8383, 8384},
	{"minecraft:pale_oak_wall_hanging_sign", 8391, 8392},
	{"minecraft:mangrove_wall_hanging_sign", 8399, 8400},
	{"minecraft:poplar_wall_hanging_sign", 8407, 8408},
	{"minecraft:crimson_wall_hanging_sign", 8415, 8416},
	{"minecraft:warped_wall_hanging_sign", 8423, 8424},
	{"minecraft:bamboo_wall_hanging_sign", 8431, 8432},
	{"minecraft:lever", 8439, 8448},
	{"minecraft:stone_pressure_plate", 8463, 8464},
	{"minecraft:iron_door", 8465, 8476},
	{"minecraft:oak_pressure_plate", 8529, 8530},
	{"minecraft:spruce_pressure_plate", 8531, 8532},
	{"minecraft:birch_pressure_plate", 8533, 8534},
	{"minecraft:jungle_pressure_plate", 8535, 8536},
	{"minecraft:acacia_pressure_plate", 8537, 8538},
	{"minecraft:cherry_pressure_plate", 8539, 8540},
	{"minecraft:dark_oak_pressure_plate", 8541, 8542},
	{"minecraft:pale_oak_pressure_plate", 8543, 8544},
	{"minecraft:mangrove_pressure_plate", 8545, 8546},
	{"minecraft:poplar_pressure_plate", 8547, 8548},
	{"minecraft:bamboo_pressure_plate", 8549, 8550},
	{"minecraft:redstone_ore", 8551, 8552},
	{"minecraft:deepslate_redstone_ore", 8553, 8554},
	{"minecraft:redstone_torch", 8555, 8555},
	{"minecraft:redstone_wall_torch", 8557, 8557},
	{"minecraft:stone_button", 8565, 8574},
	{"minecraft:snow", 8589, 8589},
	{"minecraft:ice", 8597, 8597},
	{"minecraft:snow_block", 8598, 8598},
	{"minecraft:cactus", 8599, 8599},
	{"minecraft:cactus_flower", 8615, 8615},
	{"minecraft:clay", 8616, 8616},
	{"minecraft:sugar_cane", 8617, 8617},
	{"minecraft:jukebox", 8633, 8634},
	{"minecraft:oak_fence", 8635, 8666},
	{"minecraft:netherrack", 8667, 8667},
	{"minecraft:soul_sand", 8668, 8668},
	{"minecraft:soul_soil", 8669, 8669},
	{"minecraft:basalt", 8670, 8671},
	{"minecraft:polished_basalt", 8673, 8674},
	{"minecraft:soul_torch", 8676, 8676},
	{"minecraft:soul_wall_torch", 8677, 8677},
	{"minecraft:copper_torch", 8681, 8681},
	{"minecraft:copper_wall_torch", 8682, 8682},
	{"minecraft:glowstone", 8686, 8686},
	{"minecraft:nether_portal", 8687, 8687},
	{"minecraft:carved_pumpkin", 8689, 8689},
	{"minecraft:jack_o_lantern", 8693, 8693},
	{"minecraft:cake", 8697, 8697},
	{"minecraft:repeater", 8704, 8707},
	{"minecraft:white_stained_glass", 8768, 8768},
	{"minecraft:orange_stained_glass", 8769, 8769},
	{"minecraft:magenta_stained_glass", 8770, 8770},
	{"minecraft:light_blue_stained_glass", 8771, 8771},
	{"minecraft:yellow_stained_glass", 8772, 8772},
	{"minecraft:lime_stained_glass", 8773, 8773},
	{"minecraft:pink_stained_glass", 8774, 8774},
	{"minecraft:gray_stained_glass", 8775, 8775},
	{"minecraft:light_gray_stained_glass", 8776, 8776},
	{"minecraft:cyan_stained_glass", 8777, 8777},
	{"minecraft:purple_stained_glass", 8778, 8778},
	{"minecraft:blue_stained_glass", 8779, 8779},
	{"minecraft:brown_stained_glass", 8780, 8780},
	{"minecraft:green_stained_glass", 8781, 8781},
	{"minecraft:red_stained_glass", 8782, 8782},
	{"minecraft:black_stained_glass", 8783, 8783},
	{"minecraft:oak_trapdoor", 8784, 8799},
	{"minecraft:spruce_trapdoor", 8848, 8863},
	{"minecraft:birch_trapdoor", 8912, 8927},
	{"minecraft:jungle_trapdoor", 8976, 8991},
	{"minecraft:acacia_trapdoor", 9040, 9055},
	{"minecraft:cherry_trapdoor", 9104, 9119},
	{"minecraft:dark_oak_trapdoor", 9168, 9183},
	{"minecraft:pale_oak_trapdoor", 9232, 9247},
	{"minecraft:mangrove_trapdoor", 9296, 9311},
	{"minecraft:poplar_trapdoor", 9360, 9375},
	{"minecraft:bamboo_trapdoor", 9424, 9439},
	{"minecraft:stone_bricks", 9488, 9488},
	{"minecraft:mossy_stone_bricks", 9489, 9489},
	{"minecraft:cracked_stone_bricks", 9490, 9490},
	{"minecraft:chiseled_stone_bricks", 9491, 9491},
	{"minecraft:packed_mud", 9492, 9492},
	{"minecraft:mud_bricks", 9493, 9493},
	{"minecraft:infested_stone", 9494, 9494},
	{"minecraft:infested_cobblestone", 9495, 9495},
	{"minecraft:infested_stone_bricks", 9496, 9496},
	{"minecraft:infested_mossy_stone_bricks", 9497, 9497},
	{"minecraft:infested_cracked_stone_bricks", 9498, 9498},
	{"minecraft:infested_chiseled_stone_bricks", 9499, 9499},
	{"minecraft:brown_mushroom_block", 9500, 9500},
	{"minecraft:red_mushroom_block", 9564, 9564},
	{"minecraft:mushroom_stem", 9628, 9628},
	{"minecraft:iron_bars", 9692, 9723},
	{"minecraft:copper_bars", 9724, 9755},
	{"minecraft:exposed_copper_bars", 9756, 9787},
	{"minecraft:weathered_copper_bars", 9788, 9819},
	{"minecraft:oxidized_copper_bars", 9820, 9851},
	{"minecraft:waxed_copper_bars", 9852, 9883},
	{"minecraft:waxed_exposed_copper_bars", 9884, 9915},
	{"minecraft:waxed_weathered_copper_bars", 9916, 9947},
	{"minecraft:waxed_oxidized_copper_bars", 9948, 9979},
	{"minecraft:iron_chain", 9980, 9983},
	{"minecraft:copper_chain", 9986, 9989},
	{"minecraft:exposed_copper_chain", 9992, 9995},
	{"minecraft:weathered_copper_chain", 9998, 10001},
	{"minecraft:oxidized_copper_chain", 10004, 10007},
	{"minecraft:waxed_copper_chain", 10010, 10013},
	{"minecraft:waxed_exposed_copper_chain", 10016, 10019},
	{"minecraft:waxed_weathered_copper_chain", 10022, 10025},
	{"minecraft:waxed_oxidized_copper_chain", 10028, 10031},
	{"minecraft:glass_pane", 10034, 10065},
	{"minecraft:pumpkin", 10066, 10066},
	{"minecraft:melon", 10067, 10067},
	{"minecraft:attached_pumpkin_stem", 10068, 10068},
	{"minecraft:attached_melon_stem", 10072, 10072},
	{"minecraft:pumpkin_stem", 10076, 10076},
	{"minecraft:melon_stem", 10084, 10084},
	{"minecraft:vine", 10092, 10123},
	{"minecraft:glow_lichen", 10124, 10251},
	{"minecraft:resin_clump", 10252, 10379},
	{"minecraft:oak_fence_gate", 10380, 10387},
	{"minecraft:brick_stairs", 10412, 10423},
	{"minecraft:stone_brick_stairs", 10492, 10503},
	{"minecraft:mud_brick_stairs", 10572, 10583},
	{"minecraft:mycelium", 10652, 10653},
	{"minecraft:lily_pad", 10654, 10654},
	{"minecraft:resin_block", 10655, 10655},
	{"minecraft:resin_bricks", 10656, 10656},
	{"minecraft:resin_brick_stairs", 10657, 10668},
	{"minecraft:resin_brick_slab", 10737, 10740},
	{"minecraft:resin_brick_wall", 10743, 10746},
	{"minecraft:chiseled_resin_bricks", 11067, 11067},
	{"minecraft:nether_bricks", 11068, 11068},
	{"minecraft:nether_brick_fence", 11069, 11100},
	{"minecraft:nether_brick_stairs", 11101, 11112},
	{"minecraft:nether_wart", 11181, 11181},
	{"minecraft:enchanting_table", 11185, 11185},
	{"minecraft:brewing_stand", 11186, 11193},
	{"minecraft:cauldron", 11194, 11194},
	{"minecraft:water_cauldron", 11195, 11195},
	{"minecraft:lava_cauldron", 11198, 11198},
	{"minecraft:powder_snow_cauldron", 11199, 11199},
	{"minecraft:end_portal", 11202, 11202},
	{"minecraft:end_portal_frame", 11203, 11207},
	{"minecraft:end_stone", 11211, 11211},
	{"minecraft:dragon_egg", 11212, 11212},
	{"minecraft:redstone_lamp", 11213, 11214},
	{"minecraft:cocoa", 11215, 11215},
	{"minecraft:shelf_mushroom", 11227, 11227},
	{"minecraft:sandstone_stairs", 11235, 11246},
	{"minecraft:emerald_ore", 11315, 11315},
	{"minecraft:deepslate_emerald_ore", 11316, 11316},
	{"minecraft:ender_chest", 11317, 11318},
	{"minecraft:tripwire_hook", 11325, 11334},
	{"minecraft:tripwire", 11341, 11468},
	{"minecraft:emerald_block", 11469, 11469},
	{"minecraft:spruce_stairs", 11470, 11481},
	{"minecraft:birch_stairs", 11550, 11561},
	{"minecraft:jungle_stairs", 11630, 11641},
	{"minecraft:command_block", 11710, 11716},
	{"minecraft:beacon", 11722, 11722},
	{"minecraft:cobblestone_wall", 11723, 11726},
	{"minecraft:mossy_cobblestone_wall", 12047, 12050},
	{"minecraft:flower_pot", 12371, 12371},
	{"minecraft:potted_torchflower", 12372, 12372},
	{"minecraft:potted_oak_sapling", 12373, 12373},
	{"minecraft:potted_spruce_sapling", 12374, 12374},
	{"minecraft:potted_birch_sapling", 12375, 12375},
	{"minecraft:potted_jungle_sapling", 12376, 12376},
	{"minecraft:potted_acacia_sapling", 12377, 12377},
	{"minecraft:potted_cherry_sapling", 12378, 12378},
	{"minecraft:potted_dark_oak_sapling", 12379, 12379},
	{"minecraft:potted_pale_oak_sapling", 12380, 12380},
	{"minecraft:potted_poplar_sapling", 12381, 12381},
	{"minecraft:potted_mangrove_propagule", 12382, 12382},
	{"minecraft:potted_fern", 12383, 12383},
	{"minecraft:potted_dandelion", 12384, 12384},
	{"minecraft:potted_golden_dandelion", 12385, 12385},
	{"minecraft:potted_poppy", 12386, 12386},
	{"minecraft:potted_blue_orchid", 12387, 12387},
	{"minecraft:potted_allium", 12388, 12388},
	{"minecraft:potted_azure_bluet", 12389, 12389},
	{"minecraft:potted_red_tulip", 12390, 12390},
	{"minecraft:potted_orange_tulip", 12391, 12391},
	{"minecraft:potted_white_tulip", 12392, 12392},
	{"minecraft:potted_pink_tulip", 12393, 12393},
	{"minecraft:potted_oxeye_daisy", 12394, 12394},
	{"minecraft:potted_cornflower", 12395, 12395},
	{"minecraft:potted_lily_of_the_valley", 12396, 12396},
	{"minecraft:potted_wither_rose", 12397, 12397},
	{"minecraft:potted_red_mushroom", 12398, 12398},
	{"minecraft:potted_brown_mushroom", 12399, 12399},
	{"minecraft:potted_dead_bush", 12400, 12400},
	{"minecraft:potted_cactus", 12401, 12401},
	{"minecraft:carrots", 12402, 12402},
	{"minecraft:potatoes", 12410, 12410},
	{"minecraft:oak_button", 12418, 12427},
	{"minecraft:spruce_button", 12442, 12451},
	{"minecraft:birch_button", 12466, 12475},
	{"minecraft:jungle_button", 12490, 12499},
	{"minecraft:acacia_button", 12514, 12523},
	{"minecraft:cherry_button", 12538, 12547},
	{"minecraft:dark_oak_button", 12562, 12571},
	{"minecraft:pale_oak_button", 12586, 12595},
	{"minecraft:mangrove_button", 12610, 12619},
	{"minecraft:poplar_button", 12634, 12643},
	{"minecraft:bamboo_button", 12658, 12667},
	{"minecraft:skeleton_skull", 12682, 12698},
	{"minecraft:skeleton_wall_skull", 12714, 12715},
	{"minecraft:wither_skeleton_skull", 12722, 12738},
	{"minecraft:wither_skeleton_wall_skull", 12754, 12755},
	{"minecraft:zombie_head", 12762, 12778},
	{"minecraft:zombie_wall_head", 12794, 12795},
	{"minecraft:player_head", 12802, 12818},
	{"minecraft:player_wall_head", 12834, 12835},
	{"minecraft:creeper_head", 12842, 12858},
	{"minecraft:creeper_wall_head", 12874, 12875},
	{"minecraft:dragon_head", 12882, 12898},
	{"minecraft:dragon_wall_head", 12914, 12915},
	{"minecraft:piglin_head", 12922, 12938},
	{"minecraft:piglin_wall_head", 12954, 12955},
	{"minecraft:anvil", 12962, 12962},
	{"minecraft:chipped_anvil", 12966, 12966},
	{"minecraft:damaged_anvil", 12970, 12970},
	{"minecraft:trapped_chest", 12974, 12975},
	{"minecraft:light_weighted_pressure_plate", 12998, 12998},
	{"minecraft:heavy_weighted_pressure_plate", 13014, 13014},
	{"minecraft:comparator", 13030, 13031},
	{"minecraft:daylight_detector", 13046, 13062},
	{"minecraft:redstone_block", 13078, 13078},
	{"minecraft:nether_quartz_ore", 13079, 13079},
	{"minecraft:hopper", 13080, 13080},
	{"minecraft:quartz_block", 13090, 13090},
	{"minecraft:chiseled_quartz_block", 13091, 13091},
	{"minecraft:quartz_pillar", 13092, 13093},
	{"minecraft:quartz_stairs", 13095, 13106},
	{"minecraft:activator_rail", 13175, 13188},
	{"minecraft:dropper", 13199, 13200},
	{"minecraft:white_terracotta", 13211, 13211},
	{"minecraft:orange_terracotta", 13212, 13212},
	{"minecraft:magenta_terracotta", 13213, 13213},
	{"minecraft:light_blue_terracotta", 13214, 13214},
	{"minecraft:yellow_terracotta", 13215, 13215},
	{"minecraft:lime_terracotta", 13216, 13216},
	{"minecraft:pink_terracotta", 13217, 13217},
	{"minecraft:gray_terracotta", 13218, 13218},
	{"minecraft:light_gray_terracotta", 13219, 13219},
	{"minecraft:cyan_terracotta", 13220, 13220},
	{"minecraft:purple_terracotta", 13221, 13221},
	{"minecraft:blue_terracotta", 13222, 13222},
	{"minecraft:brown_terracotta", 13223, 13223},
	{"minecraft:green_terracotta", 13224, 13224},
	{"minecraft:red_terracotta", 13225, 13225},
	{"minecraft:black_terracotta", 13226, 13226},
	{"minecraft:white_stained_glass_pane", 13227, 13258},
	{"minecraft:orange_stained_glass_pane", 13259, 13290},
	{"minecraft:magenta_stained_glass_pane", 13291, 13322},
	{"minecraft:light_blue_stained_glass_pane", 13323, 13354},
	{"minecraft:yellow_stained_glass_pane", 13355, 13386},
	{"minecraft:lime_stained_glass_pane", 13387, 13418},
	{"minecraft:pink_stained_glass_pane", 13419, 13450},
	{"minecraft:gray_stained_glass_pane", 13451, 13482},
	{"minecraft:light_gray_stained_glass_pane", 13483, 13514},
	{"minecraft:cyan_stained_glass_pane", 13515, 13546},
	{"minecraft:purple_stained_glass_pane", 13547, 13578},
	{"minecraft:blue_stained_glass_pane", 13579, 13610},
	{"minecraft:brown_stained_glass_pane", 13611, 13642},
	{"minecraft:green_stained_glass_pane", 13643, 13674},
	{"minecraft:red_stained_glass_pane", 13675, 13706},
	{"minecraft:black_stained_glass_pane", 13707, 13738},
	{"minecraft:acacia_stairs", 13739, 13750},
	{"minecraft:cherry_stairs", 13819, 13830},
	{"minecraft:dark_oak_stairs", 13899, 13910},
	{"minecraft:pale_oak_stairs", 13979, 13990},
	{"minecraft:mangrove_stairs", 14059, 14070},
	{"minecraft:poplar_stairs", 14139, 14150},
	{"minecraft:bamboo_stairs", 14219, 14230},
	{"minecraft:bamboo_mosaic_stairs", 14299, 14310},
	{"minecraft:slime_block", 14379, 14379},
	{"minecraft:barrier", 14380, 14381},
	{"minecraft:light", 14382, 14413},
	{"minecraft:iron_trapdoor", 14414, 14429},
	{"minecraft:prismarine", 14478, 14478},
	{"minecraft:prismarine_bricks", 14479, 14479},
	{"minecraft:dark_prismarine", 14480, 14480},
	{"minecraft:prismarine_stairs", 14481, 14492},
	{"minecraft:prismarine_brick_stairs", 14561, 14572},
	{"minecraft:dark_prismarine_stairs", 14641, 14652},
	{"minecraft:prismarine_slab", 14721, 14724},
	{"minecraft:prismarine_brick_slab", 14727, 14730},
	{"minecraft:dark_prismarine_slab", 14733, 14736},
	{"minecraft:sea_lantern", 14739, 14739},
	{"minecraft:hay_block", 14740, 14741},
	{"minecraft:white_carpet", 14743, 14743},
	{"minecraft:orange_carpet", 14744, 14744},
	{"minecraft:magenta_carpet", 14745, 14745},
	{"minecraft:light_blue_carpet", 14746, 14746},
	{"minecraft:yellow_carpet", 14747, 14747},
	{"minecraft:lime_carpet", 14748, 14748},
	{"minecraft:pink_carpet", 14749, 14749},
	{"minecraft:gray_carpet", 14750, 14750},
	{"minecraft:light_gray_carpet", 14751, 14751},
	{"minecraft:cyan_carpet", 14752, 14752},
	{"minecraft:purple_carpet", 14753, 14753},
	{"minecraft:blue_carpet", 14754, 14754},
	{"minecraft:brown_carpet", 14755, 14755},
	{"minecraft:green_carpet", 14756, 14756},
	{"minecraft:red_carpet", 14757, 14757},
	{"minecraft:black_carpet", 14758, 14758},
	{"minecraft:terracotta", 14759, 14759},
	{"minecraft:coal_block", 14760, 14760},
	{"minecraft:packed_ice", 14761, 14761},
	{"minecraft:sunflower", 14762, 14763},
	{"minecraft:lilac", 14764, 14765},
	{"minecraft:rose_bush", 14766, 14767},
	{"minecraft:peony", 14768, 14769},
	{"minecraft:tall_grass", 14770, 14771},
	{"minecraft:large_fern", 14772, 14773},
	{"minecraft:white_banner", 14774, 14782},
	{"minecraft:orange_banner", 14790, 14798},
	{"minecraft:magenta_banner", 14806, 14814},
	{"minecraft:light_blue_banner", 14822, 14830},
	{"minecraft:yellow_banner", 14838, 14846},
	{"minecraft:lime_banner", 14854, 14862},
	{"minecraft:pink_banner", 14870, 14878},
	{"minecraft:gray_banner", 14886, 14894},
	{"minecraft:light_gray_banner", 14902, 14910},
	{"minecraft:cyan_banner", 14918, 14926},
	{"minecraft:purple_banner", 14934, 14942},
	{"minecraft:blue_banner", 14950, 14958},
	{"minecraft:brown_banner", 14966, 14974},
	{"minecraft:green_banner", 14982, 14990},
	{"minecraft:red_banner", 14998, 15006},
	{"minecraft:black_banner", 15014, 15022},
	{"minecraft:white_wall_banner", 15030, 15030},
	{"minecraft:orange_wall_banner", 15034, 15034},
	{"minecraft:magenta_wall_banner", 15038, 15038},
	{"minecraft:light_blue_wall_banner", 15042, 15042},
	{"minecraft:yellow_wall_banner", 15046, 15046},
	{"minecraft:lime_wall_banner", 15050, 15050},
	{"minecraft:pink_wall_banner", 15054, 15054},
	{"minecraft:gray_wall_banner", 15058, 15058},
	{"minecraft:light_gray_wall_banner", 15062, 15062},
	{"minecraft:cyan_wall_banner", 15066, 15066},
	{"minecraft:purple_wall_banner", 15070, 15070},
	{"minecraft:blue_wall_banner", 15074, 15074},
	{"minecraft:brown_wall_banner", 15078, 15078},
	{"minecraft:green_wall_banner", 15082, 15082},
	{"minecraft:red_wall_banner", 15086, 15086},
	{"minecraft:black_wall_banner", 15090, 15090},
	{"minecraft:red_sandstone", 15094, 15094},
	{"minecraft:chiseled_red_sandstone", 15095, 15095},
	{"minecraft:cut_red_sandstone", 15096, 15096},
	{"minecraft:red_sandstone_stairs", 15097, 15108},
	{"minecraft:oak_slab", 15177, 15180},
	{"minecraft:spruce_slab", 15183, 15186},
	{"minecraft:birch_slab", 15189, 15192},
	{"minecraft:jungle_slab", 15195, 15198},
	{"minecraft:acacia_slab", 15201, 15204},
	{"minecraft:cherry_slab", 15207, 15210},
	{"minecraft:dark_oak_slab", 15213, 15216},
	{"minecraft:pale_oak_slab", 15219, 15222},
	{"minecraft:mangrove_slab", 15225, 15228},
	{"minecraft:poplar_slab", 15231, 15234},
	{"minecraft:bamboo_slab", 15237, 15240},
	{"minecraft:bamboo_mosaic_slab", 15243, 15246},
	{"minecraft:stone_slab", 15249, 15252},
	{"minecraft:sandstone_slab", 15255, 15258},
	{"minecraft:cut_sandstone_slab", 15261, 15264},
	{"minecraft:petrified_oak_slab", 15267, 15270},
	{"minecraft:cobblestone_slab", 15273, 15276},
	{"minecraft:brick_slab", 15279, 15282},
	{"minecraft:stone_brick_slab", 15285, 15288},
	{"minecraft:mud_brick_slab", 15291, 15294},
	{"minecraft:nether_brick_slab", 15297, 15300},
	{"minecraft:quartz_slab", 15303, 15306},
	{"minecraft:red_sandstone_slab", 15309, 15312},
	{"minecraft:cut_red_sandstone_slab", 15315, 15318},
	{"minecraft:smooth_stone", 15321, 15321},
	{"minecraft:smooth_stone_slab", 15322, 15325},
	{"minecraft:smooth_sandstone", 15328, 15328},
	{"minecraft:smooth_quartz", 15329, 15329},
	{"minecraft:smooth_red_sandstone", 15330, 15330},
	{"minecraft:spruce_fence_gate", 15331, 15338},
	{"minecraft:birch_fence_gate", 15363, 15370},
	{"minecraft:jungle_fence_gate", 15395, 15402},
	{"minecraft:acacia_fence_gate", 15427, 15434},
	{"minecraft:cherry_fence_gate", 15459, 15466},
	{"minecraft:dark_oak_fence_gate", 15491, 15498},
	{"minecraft:pale_oak_fence_gate", 15523, 15530},
	{"minecraft:mangrove_fence_gate", 15555, 15562},
	{"minecraft:poplar_fence_gate", 15587, 15594},
	{"minecraft:bamboo_fence_gate", 15619, 15626},
	{"minecraft:spruce_fence", 15651, 15682},
	{"minecraft:birch_fence", 15683, 15714},
	{"minecraft:jungle_fence", 15715, 15746},
	{"minecraft:acacia_fence", 15747, 15778},
	{"minecraft:cherry_fence", 15779, 15810},
	{"minecraft:dark_oak_fence", 15811, 15842},
	{"minecraft:pale_oak_fence", 15843, 15874},
	{"minecraft:mangrove_fence", 15875, 15906},
	{"minecraft:poplar_fence", 15907, 15938},
	{"minecraft:bamboo_fence", 15939, 15970},
	{"minecraft:spruce_door", 15971, 15982},
	{"minecraft:birch_door", 16035, 16046},
	{"minecraft:jungle_door", 16099, 16110},
	{"minecraft:acacia_door", 16163, 16174},
	{"minecraft:cherry_door", 16227, 16238},
	{"minecraft:dark_oak_door", 16291, 16302},
	{"minecraft:pale_oak_door", 16355, 16366},
	{"minecraft:mangrove_door", 16419, 16430},
	{"minecraft:poplar_door", 16483, 16494},
	{"minecraft:bamboo_door", 16547, 16558},
	{"minecraft:end_rod", 16611, 16615},
	{"minecraft:chorus_plant", 16617, 16680},
	{"minecraft:chorus_flower", 16681, 16681},
	{"minecraft:purpur_block", 16687, 16687},
	{"minecraft:purpur_slab", 16688, 16691},
	{"minecraft:purpur_pillar", 16694, 16695},
	{"minecraft:purpur_stairs", 16697, 16708},
	{"minecraft:end_stone_bricks", 16777, 16777},
	{"minecraft:torchflower_crop", 16778, 16778},
	{"minecraft:pitcher_crop", 16780, 16781},
	{"minecraft:pitcher_plant", 16790, 16791},
	{"minecraft:beetroots", 16792, 16792},
	{"minecraft:dirt_path", 16796, 16796},
	{"minecraft:end_gateway", 16797, 16797},
	{"minecraft:repeating_command_block", 16798, 16804},
	{"minecraft:chain_command_block", 16810, 16816},
	{"minecraft:frosted_ice", 16822, 16822},
	{"minecraft:magma_block", 16826, 16826},
	{"minecraft:nether_wart_block", 16827, 16827},
	{"minecraft:red_nether_bricks", 16828, 16828},
	{"minecraft:bone_block", 16829, 16830},
	{"minecraft:structure_void", 16832, 16832},
	{"minecraft:observer", 16833, 16838},
	{"minecraft:shulker_box", 16845, 16849},
	{"minecraft:white_shulker_box", 16851, 16855},
	{"minecraft:orange_shulker_box", 16857, 16861},
	{"minecraft:magenta_shulker_box", 16863, 16867},
	{"minecraft:light_blue_shulker_box", 16869, 16873},
	{"minecraft:yellow_shulker_box", 16875, 16879},
	{"minecraft:lime_shulker_box", 16881, 16885},
	{"minecraft:pink_shulker_box", 16887, 16891},
	{"minecraft:gray_shulker_box", 16893, 16897},
	{"minecraft:light_gray_shulker_box", 16899, 16903},
	{"minecraft:cyan_shulker_box", 16905, 16909},
	{"minecraft:purple_shulker_box", 16911, 16915},
	{"minecraft:blue_shulker_box", 16917, 16921},
	{"minecraft:brown_shulker_box", 16923, 16927},
	{"minecraft:green_shulker_box", 16929, 16933},
	{"minecraft:red_shulker_box", 16935, 16939},
	{"minecraft:black_shulker_box", 16941, 16945},
	{"minecraft:white_glazed_terracotta", 16947, 16947},
	{"minecraft:orange_glazed_terracotta", 16951, 16951},
	{"minecraft:magenta_glazed_terracotta", 16955, 16955},
	{"minecraft:light_blue_glazed_terracotta", 16959, 16959},
	{"minecraft:yellow_glazed_terracotta", 16963, 16963},
	{"minecraft:lime_glazed_terracotta", 16967, 16967},
	{"minecraft:pink_glazed_terracotta", 16971, 16971},
	{"minecraft:gray_glazed_terracotta", 16975, 16975},
	{"minecraft:light_gray_glazed_terracotta", 16979, 16979},
	{"minecraft:cyan_glazed_terracotta", 16983, 16983},
	{"minecraft:purple_glazed_terracotta", 16987, 16987},
	{"minecraft:blue_glazed_terracotta", 16991, 16991},
	{"minecraft:brown_glazed_terracotta", 16995, 16995},
	{"minecraft:green_glazed_terracotta", 16999, 16999},
	{"minecraft:red_glazed_terracotta", 17003, 17003},
	{"minecraft:black_glazed_terracotta", 17007, 17007},
	{"minecraft:white_concrete", 17011, 17011},
	{"minecraft:orange_concrete", 17012, 17012},
	{"minecraft:magenta_concrete", 17013, 17013},
	{"minecraft:light_blue_concrete", 17014, 17014},
	{"minecraft:yellow_concrete", 17015, 17015},
	{"minecraft:lime_concrete", 17016, 17016},
	{"minecraft:pink_concrete", 17017, 17017},
	{"minecraft:gray_concrete", 17018, 17018},
	{"minecraft:light_gray_concrete", 17019, 17019},
	{"minecraft:cyan_concrete", 17020, 17020},
	{"minecraft:purple_concrete", 17021, 17021},
	{"minecraft:blue_concrete", 17022, 17022},
	{"minecraft:brown_concrete", 17023, 17023},
	{"minecraft:green_concrete", 17024, 17024},
	{"minecraft:red_concrete", 17025, 17025},
	{"minecraft:black_concrete", 17026, 17026},
	{"minecraft:white_concrete_stairs", 17027, 17038},
	{"minecraft:orange_concrete_stairs", 17107, 17118},
	{"minecraft:magenta_concrete_stairs", 17187, 17198},
	{"minecraft:light_blue_concrete_stairs", 17267, 17278},
	{"minecraft:yellow_concrete_stairs", 17347, 17358},
	{"minecraft:lime_concrete_stairs", 17427, 17438},
	{"minecraft:pink_concrete_stairs", 17507, 17518},
	{"minecraft:gray_concrete_stairs", 17587, 17598},
	{"minecraft:light_gray_concrete_stairs", 17667, 17678},
	{"minecraft:cyan_concrete_stairs", 17747, 17758},
	{"minecraft:purple_concrete_stairs", 17827, 17838},
	{"minecraft:blue_concrete_stairs", 17907, 17918},
	{"minecraft:brown_concrete_stairs", 17987, 17998},
	{"minecraft:green_concrete_stairs", 18067, 18078},
	{"minecraft:red_concrete_stairs", 18147, 18158},
	{"minecraft:black_concrete_stairs", 18227, 18238},
	{"minecraft:white_concrete_slab", 18307, 18310},
	{"minecraft:orange_concrete_slab", 18313, 18316},
	{"minecraft:magenta_concrete_slab", 18319, 18322},
	{"minecraft:light_blue_concrete_slab", 18325, 18328},
	{"minecraft:yellow_concrete_slab", 18331, 18334},
	{"minecraft:lime_concrete_slab", 18337, 18340},
	{"minecraft:pink_concrete_slab", 18343, 18346},
	{"minecraft:gray_concrete_slab", 18349, 18352},
	{"minecraft:light_gray_concrete_slab", 18355, 18358},
	{"minecraft:cyan_concrete_slab", 18361, 18364},
	{"minecraft:purple_concrete_slab", 18367, 18370},
	{"minecraft:blue_concrete_slab", 18373, 18376},
	{"minecraft:brown_concrete_slab", 18379, 18382},
	{"minecraft:green_concrete_slab", 18385, 18388},
	{"minecraft:red_concrete_slab", 18391, 18394},
	{"minecraft:black_concrete_slab", 18397, 18400},
	{"minecraft:white_concrete_powder", 18403, 18403},
	{"minecraft:orange_concrete_powder", 18404, 18404},
	{"minecraft:magenta_concrete_powder", 18405, 18405},
	{"minecraft:light_blue_concrete_powder", 18406, 18406},
	{"minecraft:yellow_concrete_powder", 18407, 18407},
	{"minecraft:lime_concrete_powder", 18408, 18408},
	{"minecraft:pink_concrete_powder", 18409, 18409},
	{"minecraft:gray_concrete_powder", 18410, 18410},
	{"minecraft:light_gray_concrete_powder", 18411, 18411},
	{"minecraft:cyan_concrete_powder", 18412, 18412},
	{"minecraft:purple_concrete_powder", 18413, 18413},
	{"minecraft:blue_concrete_powder", 18414, 18414},
	{"minecraft:brown_concrete_powder", 18415, 18415},
	{"minecraft:green_concrete_powder", 18416, 18416},
	{"minecraft:red_concrete_powder", 18417, 18417},
	{"minecraft:black_concrete_powder", 18418, 18418},
	{"minecraft:kelp", 18419, 18419},
	{"minecraft:kelp_plant", 18445, 18445},
	{"minecraft:dried_kelp_block", 18446, 18446},
	{"minecraft:turtle_egg", 18447, 18447},
	{"minecraft:sniffer_egg", 18459, 18459},
	{"minecraft:dried_ghast", 18462, 18463},
	{"minecraft:dead_tube_coral_block", 18494, 18494},
	{"minecraft:dead_brain_coral_block", 18495, 18495},
	{"minecraft:dead_bubble_coral_block", 18496, 18496},
	{"minecraft:dead_fire_coral_block", 18497, 18497},
	{"minecraft:dead_horn_coral_block", 18498, 18498},
	{"minecraft:tube_coral_block", 18499, 18499},
	{"minecraft:brain_coral_block", 18500, 18500},
	{"minecraft:bubble_coral_block", 18501, 18501},
	{"minecraft:fire_coral_block", 18502, 18502},
	{"minecraft:horn_coral_block", 18503, 18503},
	{"minecraft:dead_tube_coral", 18504, 18504},
	{"minecraft:dead_brain_coral", 18506, 18506},
	{"minecraft:dead_bubble_coral", 18508, 18508},
	{"minecraft:dead_fire_coral", 18510, 18510},
	{"minecraft:dead_horn_coral", 18512, 18512},
	{"minecraft:tube_coral", 18514, 18514},
	{"minecraft:brain_coral", 18516, 18516},
	{"minecraft:bubble_coral", 18518, 18518},
	{"minecraft:fire_coral", 18520, 18520},
	{"minecraft:horn_coral", 18522, 18522},
	{"minecraft:dead_tube_coral_fan", 18524, 18524},
	{"minecraft:dead_brain_coral_fan", 18526, 18526},
	{"minecraft:dead_bubble_coral_fan", 18528, 18528},
	{"minecraft:dead_fire_coral_fan", 18530, 18530},
	{"minecraft:dead_horn_coral_fan", 18532, 18532},
	{"minecraft:tube_coral_fan", 18534, 18534},
	{"minecraft:brain_coral_fan", 18536, 18536},
	{"minecraft:bubble_coral_fan", 18538, 18538},
	{"minecraft:fire_coral_fan", 18540, 18540},
	{"minecraft:horn_coral_fan", 18542, 18542},
	{"minecraft:dead_tube_coral_wall_fan", 18544, 18544},
	{"minecraft:dead_brain_coral_wall_fan", 18552, 18552},
	{"minecraft:dead_bubble_coral_wall_fan", 18560, 18560},
	{"minecraft:dead_fire_coral_wall_fan", 18568, 18568},
	{"minecraft:dead_horn_coral_wall_fan", 18576, 18576},
	{"minecraft:tube_coral_wall_fan", 18584, 18584},
	{"minecraft:brain_coral_wall_fan", 18592, 18592},
	{"minecraft:bubble_coral_wall_fan", 18600, 18600},
	{"minecraft:fire_coral_wall_fan", 18608, 18608},
	{"minecraft:horn_coral_wall_fan", 18616, 18616},
	{"minecraft:sea_pickle", 18624, 18624},
	{"minecraft:blue_ice", 18632, 18632},
	{"minecraft:conduit", 18633, 18633},
	{"minecraft:bamboo_sapling", 18635, 18635},
	{"minecraft:bamboo", 18636, 18636},
	{"minecraft:potted_bamboo", 18648, 18648},
	{"minecraft:void_air", 18649, 18649},
	{"minecraft:cave_air", 18650, 18650},
	{"minecraft:bubble_column", 18651, 18651},
	{"minecraft:polished_granite_stairs", 18653, 18664},
	{"minecraft:smooth_red_sandstone_stairs", 18733, 18744},
	{"minecraft:mossy_stone_brick_stairs", 18813, 18824},
	{"minecraft:polished_diorite_stairs", 18893, 18904},
	{"minecraft:mossy_cobblestone_stairs", 18973, 18984},
	{"minecraft:end_stone_brick_stairs", 19053, 19064},
	{"minecraft:stone_stairs", 19133, 19144},
	{"minecraft:smooth_sandstone_stairs", 19213, 19224},
	{"minecraft:smooth_quartz_stairs", 19293, 19304},
	{"minecraft:granite_stairs", 19373, 19384},
	{"minecraft:andesite_stairs", 19453, 19464},
	{"minecraft:red_nether_brick_stairs", 19533, 19544},
	{"minecraft:polished_andesite_stairs", 19613, 19624},
	{"minecraft:diorite_stairs", 19693, 19704},
	{"minecraft:polished_granite_slab", 19773, 19776},
	{"minecraft:smooth_red_sandstone_slab", 19779, 19782},
	{"minecraft:mossy_stone_brick_slab", 19785, 19788},
	{"minecraft:polished_diorite_slab", 19791, 19794},
	{"minecraft:mossy_cobblestone_slab", 19797, 19800},
	{"minecraft:end_stone_brick_slab", 19803, 19806},
	{"minecraft:smooth_sandstone_slab", 19809, 19812},
	{"minecraft:smooth_quartz_slab", 19815, 19818},
	{"minecraft:granite_slab", 19821, 19824},
	{"minecraft:andesite_slab", 19827, 19830},
	{"minecraft:red_nether_brick_slab", 19833, 19836},
	{"minecraft:polished_andesite_slab", 19839, 19842},
	{"minecraft:diorite_slab", 19845, 19848},
	{"minecraft:brick_wall", 19851, 19854},
	{"minecraft:prismarine_wall", 20175, 20178},
	{"minecraft:red_sandstone_wall", 20499, 20502},
	{"minecraft:mossy_stone_brick_wall", 20823, 20826},
	{"minecraft:granite_wall", 21147, 21150},
	{"minecraft:stone_brick_wall", 21471, 21474},
	{"minecraft:mud_brick_wall", 21795, 21798},
	{"minecraft:nether_brick_wall", 22119, 22122},
	{"minecraft:andesite_wall", 22443, 22446},
	{"minecraft:red_nether_brick_wall", 22767, 22770},
	{"minecraft:sandstone_wall", 23091, 23094},
	{"minecraft:end_stone_brick_wall", 23415, 23418},
	{"minecraft:diorite_wall", 23739, 23742},
	{"minecraft:scaffolding", 24063, 24094},
	{"minecraft:loom", 24095, 24095},
	{"minecraft:barrel", 24099, 24100},
	{"minecraft:smoker", 24111, 24112},
	{"minecraft:blast_furnace", 24119, 24120},
	{"minecraft:cartography_table", 24127, 24127},
	{"minecraft:fletching_table", 24128, 24128},
	{"minecraft:grindstone", 24129, 24133},
	{"minecraft:lectern", 24141, 24144},
	{"minecraft:smithing_table", 24157, 24157},
	{"minecraft:stonecutter", 24158, 24158},
	{"minecraft:bell", 24162, 24163},
	{"minecraft:lantern", 24194, 24197},
	{"minecraft:soul_lantern", 24198, 24201},
	{"minecraft:copper_lantern", 24202, 24205},
	{"minecraft:exposed_copper_lantern", 24206, 24209},
	{"minecraft:weathered_copper_lantern", 24210, 24213},
	{"minecraft:oxidized_copper_lantern", 24214, 24217},
	{"minecraft:waxed_copper_lantern", 24218, 24221},
	{"minecraft:waxed_exposed_copper_lantern", 24222, 24225},
	{"minecraft:waxed_weathered_copper_lantern", 24226, 24229},
	{"minecraft:waxed_oxidized_copper_lantern", 24230, 24233},
	{"minecraft:campfire", 24234, 24237},
	{"minecraft:soul_campfire", 24266, 24269},
	{"minecraft:sweet_berry_bush", 24298, 24298},
	{"minecraft:warped_stem", 24302, 24303},
	{"minecraft:stripped_warped_stem", 24305, 24306},
	{"minecraft:warped_hyphae", 24308, 24309},
	{"minecraft:stripped_warped_hyphae", 24311, 24312},
	{"minecraft:warped_nylium", 24314, 24314},
	{"minecraft:warped_fungus", 24315, 24315},
	{"minecraft:warped_wart_block", 24316, 24316},
	{"minecraft:warped_roots", 24317, 24317},
	{"minecraft:nether_sprouts", 24318, 24318},
	{"minecraft:crimson_stem", 24319, 24320},
	{"minecraft:stripped_crimson_stem", 24322, 24323},
	{"minecraft:crimson_hyphae", 24325, 24326},
	{"minecraft:stripped_crimson_hyphae", 24328, 24329},
	{"minecraft:crimson_nylium", 24331, 24331},
	{"minecraft:crimson_fungus", 24332, 24332},
	{"minecraft:shroomlight", 24333, 24333},
	{"minecraft:weeping_vines", 24334, 24334},
	{"minecraft:weeping_vines_plant", 24360, 24360},
	{"minecraft:twisting_vines", 24361, 24361},
	{"minecraft:twisting_vines_plant", 24387, 24387},
	{"minecraft:crimson_roots", 24388, 24388},
	{"minecraft:crimson_planks", 24389, 24389},
	{"minecraft:warped_planks", 24390, 24390},
	{"minecraft:crimson_slab", 24391, 24394},
	{"minecraft:warped_slab", 24397, 24400},
	{"minecraft:crimson_pressure_plate", 24403, 24404},
	{"minecraft:warped_pressure_plate", 24405, 24406},
	{"minecraft:crimson_fence", 24407, 24438},
	{"minecraft:warped_fence", 24439, 24470},
	{"minecraft:crimson_trapdoor", 24471, 24486},
	{"minecraft:warped_trapdoor", 24535, 24550},
	{"minecraft:crimson_fence_gate", 24599, 24606},
	{"minecraft:warped_fence_gate", 24631, 24638},
	{"minecraft:crimson_stairs", 24663, 24674},
	{"minecraft:warped_stairs", 24743, 24754},
	{"minecraft:crimson_button", 24823, 24832},
	{"minecraft:warped_button", 24847, 24856},
	{"minecraft:crimson_door", 24871, 24882},
	{"minecraft:warped_door", 24935, 24946},
	{"minecraft:crimson_sign", 24999, 25016},
	{"minecraft:warped_sign", 25031, 25048},
	{"minecraft:crimson_wall_sign", 25063, 25064},
	{"minecraft:warped_wall_sign", 25071, 25072},
	{"minecraft:structure_block", 25079, 25080},
	{"minecraft:jigsaw", 25083, 25093},
	{"minecraft:test_block", 25095, 25095},
	{"minecraft:test_instance_block", 25099, 25099},
	{"minecraft:composter", 25100, 25100},
	{"minecraft:target", 25109, 25109},
	{"minecraft:bee_nest", 25125, 25125},
	{"minecraft:beehive", 25149, 25149},
	{"minecraft:honey_block", 25173, 25173},
	{"minecraft:honeycomb_block", 25174, 25174},
	{"minecraft:netherite_block", 25175, 25175},
	{"minecraft:ancient_debris", 25176, 25176},
	{"minecraft:crying_obsidian", 25177, 25177},
	{"minecraft:respawn_anchor", 25178, 25178},
	{"minecraft:potted_crimson_fungus", 25183, 25183},
	{"minecraft:potted_warped_fungus", 25184, 25184},
	{"minecraft:potted_crimson_roots", 25185, 25185},
	{"minecraft:potted_warped_roots", 25186, 25186},
	{"minecraft:lodestone", 25187, 25187},
	{"minecraft:blackstone", 25188, 25188},
	{"minecraft:blackstone_stairs", 25189, 25200},
	{"minecraft:blackstone_wall", 25269, 25272},
	{"minecraft:blackstone_slab", 25593, 25596},
	{"minecraft:polished_blackstone", 25599, 25599},
	{"minecraft:polished_blackstone_bricks", 25600, 25600},
	{"minecraft:cracked_polished_blackstone_bricks", 25601, 25601},
	{"minecraft:chiseled_polished_blackstone", 25602, 25602},
	{"minecraft:polished_blackstone_brick_slab", 25603, 25606},
	{"minecraft:polished_blackstone_brick_stairs", 25609, 25620},
	{"minecraft:polished_blackstone_brick_wall", 25689, 25692},
	{"minecraft:gilded_blackstone", 26013, 26013},
	{"minecraft:polished_blackstone_stairs", 26014, 26025},
	{"minecraft:polished_blackstone_slab", 26094, 26097},
	{"minecraft:polished_blackstone_pressure_plate", 26100, 26101},
	{"minecraft:polished_blackstone_button", 26102, 26111},
	{"minecraft:polished_blackstone_wall", 26126, 26129},
	{"minecraft:chiseled_nether_bricks", 26450, 26450},
	{"minecraft:cracked_nether_bricks", 26451, 26451},
	{"minecraft:quartz_bricks", 26452, 26452},
	{"minecraft:candle", 26453, 26456},
	{"minecraft:white_candle", 26469, 26472},
	{"minecraft:orange_candle", 26485, 26488},
	{"minecraft:magenta_candle", 26501, 26504},
	{"minecraft:light_blue_candle", 26517, 26520},
	{"minecraft:yellow_candle", 26533, 26536},
	{"minecraft:lime_candle", 26549, 26552},
	{"minecraft:pink_candle", 26565, 26568},
	{"minecraft:gray_candle", 26581, 26584},
	{"minecraft:light_gray_candle", 26597, 26600},
	{"minecraft:cyan_candle", 26613, 26616},
	{"minecraft:purple_candle", 26629, 26632},
	{"minecraft:blue_candle", 26645, 26648},
	{"minecraft:brown_candle", 26661, 26664},
	{"minecraft:green_candle", 26677, 26680},
	{"minecraft:red_candle", 26693, 26696},
	{"minecraft:black_candle", 26709, 26712},
	{"minecraft:candle_cake", 26725, 26726},
	{"minecraft:white_candle_cake", 26727, 26728},
	{"minecraft:orange_candle_cake", 26729, 26730},
	{"minecraft:magenta_candle_cake", 26731, 26732},
	{"minecraft:light_blue_candle_cake", 26733, 26734},
	{"minecraft:yellow_candle_cake", 26735, 26736},
	{"minecraft:lime_candle_cake", 26737, 26738},
	{"minecraft:pink_candle_cake", 26739, 26740},
	{"minecraft:gray_candle_cake", 26741, 26742},
	{"minecraft:light_gray_candle_cake", 26743, 26744},
	{"minecraft:cyan_candle_cake", 26745, 26746},
	{"minecraft:purple_candle_cake", 26747, 26748},
	{"minecraft:blue_candle_cake", 26749, 26750},
	{"minecraft:brown_candle_cake", 26751, 26752},
	{"minecraft:green_candle_cake", 26753, 26754},
	{"minecraft:red_candle_cake", 26755, 26756},
	{"minecraft:black_candle_cake", 26757, 26758},
	{"minecraft:amethyst_block", 26759, 26759},
	{"minecraft:budding_amethyst", 26760, 26760},
	{"minecraft:amethyst_cluster", 26761, 26770},
	{"minecraft:large_amethyst_bud", 26773, 26782},
	{"minecraft:medium_amethyst_bud", 26785, 26794},
	{"minecraft:small_amethyst_bud", 26797, 26806},
	{"minecraft:tuff", 26809, 26809},
	{"minecraft:tuff_slab", 26810, 26813},
	{"minecraft:tuff_stairs", 26816, 26827},
	{"minecraft:tuff_wall", 26896, 26899},
	{"minecraft:polished_tuff", 27220, 27220},
	{"minecraft:polished_tuff_slab", 27221, 27224},
	{"minecraft:polished_tuff_stairs", 27227, 27238},
	{"minecraft:polished_tuff_wall", 27307, 27310},
	{"minecraft:chiseled_tuff", 27631, 27631},
	{"minecraft:tuff_bricks", 27632, 27632},
	{"minecraft:tuff_brick_slab", 27633, 27636},
	{"minecraft:tuff_brick_stairs", 27639, 27650},
	{"minecraft:tuff_brick_wall", 27719, 27722},
	{"minecraft:chiseled_tuff_bricks", 28043, 28043},
	{"minecraft:sulfur", 28044, 28044},
	{"minecraft:potent_sulfur", 28045, 28045},
	{"minecraft:sulfur_slab", 28050, 28053},
	{"minecraft:sulfur_stairs", 28056, 28067},
	{"minecraft:sulfur_wall", 28136, 28139},
	{"minecraft:polished_sulfur", 28460, 28460},
	{"minecraft:polished_sulfur_slab", 28461, 28464},
	{"minecraft:polished_sulfur_stairs", 28467, 28478},
	{"minecraft:polished_sulfur_wall", 28547, 28550},
	{"minecraft:sulfur_bricks", 28871, 28871},
	{"minecraft:sulfur_brick_slab", 28872, 28875},
	{"minecraft:sulfur_brick_stairs", 28878, 28889},
	{"minecraft:sulfur_brick_wall", 28958, 28961},
	{"minecraft:chiseled_sulfur", 29282, 29282},
	{"minecraft:cinnabar", 29283, 29283},
	{"minecraft:cinnabar_slab", 29284, 29287},
	{"minecraft:cinnabar_stairs", 29290, 29301},
	{"minecraft:cinnabar_wall", 29370, 29373},
	{"minecraft:polished_cinnabar", 29694, 29694},
	{"minecraft:polished_cinnabar_slab", 29695, 29698},
	{"minecraft:polished_cinnabar_stairs", 29701, 29712},
	{"minecraft:polished_cinnabar_wall", 29781, 29784},
	{"minecraft:cinnabar_bricks", 30105, 30105},
	{"minecraft:cinnabar_brick_slab", 30106, 30109},
	{"minecraft:cinnabar_brick_stairs", 30112, 30123},
	{"minecraft:cinnabar_brick_wall", 30192, 30195},
	{"minecraft:chiseled_cinnabar", 30516, 30516},
	{"minecraft:calcite", 30517, 30517},
	{"minecraft:tinted_glass", 30518, 30518},
	{"minecraft:powder_snow", 30519, 30519},
	{"minecraft:sculk_sensor", 30520, 30521},
	{"minecraft:calibrated_sculk_sensor", 30616, 30617},
	{"minecraft:sculk", 31000, 31000},
	{"minecraft:sculk_vein", 31001, 31128},
	{"minecraft:sculk_catalyst", 31129, 31130},
	{"minecraft:sculk_shrieker", 31131, 31138},
	{"minecraft:copper_block", 31139, 31139},
	{"minecraft:exposed_copper", 31140, 31140},
	{"minecraft:weathered_copper", 31141, 31141},
	{"minecraft:oxidized_copper", 31142, 31142},
	{"minecraft:waxed_copper_block", 31143, 31143},
	{"minecraft:waxed_exposed_copper", 31144, 31144},
	{"minecraft:waxed_weathered_copper", 31145, 31145},
	{"minecraft:waxed_oxidized_copper", 31146, 31146},
	{"minecraft:copper_ore", 31147, 31147},
	{"minecraft:deepslate_copper_ore", 31148, 31148},
	{"minecraft:cut_copper", 31149, 31149},
	{"minecraft:exposed_cut_copper", 31150, 31150},
	{"minecraft:weathered_cut_copper", 31151, 31151},
	{"minecraft:oxidized_cut_copper", 31152, 31152},
	{"minecraft:waxed_cut_copper", 31153, 31153},
	{"minecraft:waxed_exposed_cut_copper", 31154, 31154},
	{"minecraft:waxed_weathered_cut_copper", 31155, 31155},
	{"minecraft:waxed_oxidized_cut_copper", 31156, 31156},
	{"minecraft:chiseled_copper", 31157, 31157},
	{"minecraft:exposed_chiseled_copper", 31158, 31158},
	{"minecraft:weathered_chiseled_copper", 31159, 31159},
	{"minecraft:oxidized_chiseled_copper", 31160, 31160},
	{"minecraft:waxed_chiseled_copper", 31161, 31161},
	{"minecraft:waxed_exposed_chiseled_copper", 31162, 31162},
	{"minecraft:waxed_weathered_chiseled_copper", 31163, 31163},
	{"minecraft:waxed_oxidized_chiseled_copper", 31164, 31164},
	{"minecraft:cut_copper_stairs", 31165, 31176},
	{"minecraft:exposed_cut_copper_stairs", 31245, 31256},
	{"minecraft:weathered_cut_copper_stairs", 31325, 31336},
	{"minecraft:oxidized_cut_copper_stairs", 31405, 31416},
	{"minecraft:waxed_cut_copper_stairs", 31485, 31496},
	{"minecraft:waxed_exposed_cut_copper_stairs", 31565, 31576},
	{"minecraft:waxed_weathered_cut_copper_stairs", 31645, 31656},
	{"minecraft:waxed_oxidized_cut_copper_stairs", 31725, 31736},
	{"minecraft:cut_copper_slab", 31805, 31808},
	{"minecraft:exposed_cut_copper_slab", 31811, 31814},
	{"minecraft:weathered_cut_copper_slab", 31817, 31820},
	{"minecraft:oxidized_cut_copper_slab", 31823, 31826},
	{"minecraft:waxed_cut_copper_slab", 31829, 31832},
	{"minecraft:waxed_exposed_cut_copper_slab", 31835, 31838},
	{"minecraft:waxed_weathered_cut_copper_slab", 31841, 31844},
	{"minecraft:waxed_oxidized_cut_copper_slab", 31847, 31850},
	{"minecraft:copper_door", 31853, 31864},
	{"minecraft:exposed_copper_door", 31917, 31928},
	{"minecraft:weathered_copper_door", 31981, 31992},
	{"minecraft:oxidized_copper_door", 32045, 32056},
	{"minecraft:waxed_copper_door", 32109, 32120},
	{"minecraft:waxed_exposed_copper_door", 32173, 32184},
	{"minecraft:waxed_weathered_copper_door", 32237, 32248},
	{"minecraft:waxed_oxidized_copper_door", 32301, 32312},
	{"minecraft:copper_trapdoor", 32365, 32380},
	{"minecraft:exposed_copper_trapdoor", 32429, 32444},
	{"minecraft:weathered_copper_trapdoor", 32493, 32508},
	{"minecraft:oxidized_copper_trapdoor", 32557, 32572},
	{"minecraft:waxed_copper_trapdoor", 32621, 32636},
	{"minecraft:waxed_exposed_copper_trapdoor", 32685, 32700},
	{"minecraft:waxed_weathered_copper_trapdoor", 32749, 32764},
	{"minecraft:waxed_oxidized_copper_trapdoor", 32813, 32828},
	{"minecraft:copper_grate", 32877, 32878},
	{"minecraft:exposed_copper_grate", 32879, 32880},
	{"minecraft:weathered_copper_grate", 32881, 32882},
	{"minecraft:oxidized_copper_grate", 32883, 32884},
	{"minecraft:waxed_copper_grate", 32885, 32886},
	{"minecraft:waxed_exposed_copper_grate", 32887, 32888},
	{"minecraft:waxed_weathered_copper_grate", 32889, 32890},
	{"minecraft:waxed_oxidized_copper_grate", 32891, 32892},
	{"minecraft:copper_bulb", 32893, 32896},
	{"minecraft:exposed_copper_bulb", 32897, 32900},
	{"minecraft:weathered_copper_bulb", 32901, 32904},
	{"minecraft:oxidized_copper_bulb", 32905, 32908},
	{"minecraft:waxed_copper_bulb", 32909, 32912},
	{"minecraft:waxed_exposed_copper_bulb", 32913, 32916},
	{"minecraft:waxed_weathered_copper_bulb", 32917, 32920},
	{"minecraft:waxed_oxidized_copper_bulb", 32921, 32924},
	{"minecraft:copper_chest", 32925, 32926},
	{"minecraft:exposed_copper_chest", 32949, 32950},
	{"minecraft:weathered_copper_chest", 32973, 32974},
	{"minecraft:oxidized_copper_chest", 32997, 32998},
	{"minecraft:waxed_copper_chest", 33021, 33022},
	{"minecraft:waxed_exposed_copper_chest", 33045, 33046},
	{"minecraft:waxed_weathered_copper_chest", 33069, 33070},
	{"minecraft:waxed_oxidized_copper_chest", 33093, 33094},
	{"minecraft:copper_golem_statue", 33117, 33118},
	{"minecraft:exposed_copper_golem_statue", 33149, 33150},
	{"minecraft:weathered_copper_golem_statue", 33181, 33182},
	{"minecraft:oxidized_copper_golem_statue", 33213, 33214},
	{"minecraft:waxed_copper_golem_statue", 33245, 33246},
	{"minecraft:waxed_exposed_copper_golem_statue", 33277, 33278},
	{"minecraft:waxed_weathered_copper_golem_statue", 33309, 33310},
	{"minecraft:waxed_oxidized_copper_golem_statue", 33341, 33342},
	{"minecraft:lightning_rod", 33373, 33392},
	{"minecraft:exposed_lightning_rod", 33397, 33416},
	{"minecraft:weathered_lightning_rod", 33421, 33440},
	{"minecraft:oxidized_lightning_rod", 33445, 33464},
	{"minecraft:waxed_lightning_rod", 33469, 33488},
	{"minecraft:waxed_exposed_lightning_rod", 33493, 33512},
	{"minecraft:waxed_weathered_lightning_rod", 33517, 33536},
	{"minecraft:waxed_oxidized_lightning_rod", 33541, 33560},
	{"minecraft:dripstone_block", 33565, 33565},
	{"minecraft:pointed_dripstone", 33566, 33571},
	{"minecraft:sulfur_spike", 33586, 33591},
	{"minecraft:cave_vines", 33606, 33607},
	{"minecraft:cave_vines_plant", 33658, 33659},
	{"minecraft:spore_blossom", 33660, 33660},
	{"minecraft:azalea", 33661, 33661},
	{"minecraft:flowering_azalea", 33662, 33662},
	{"minecraft:moss_carpet", 33663, 33663},
	{"minecraft:pink_petals", 33664, 33664},
	{"minecraft:wildflowers", 33680, 33680},
	{"minecraft:leaf_litter", 33696, 33696},
	{"minecraft:moss_block", 33712, 33712},
	{"minecraft:big_dripleaf", 33713, 33714},
	{"minecraft:big_dripleaf_stem", 33745, 33746},
	{"minecraft:small_dripleaf", 33753, 33756},
	{"minecraft:hanging_roots", 33769, 33770},
	{"minecraft:rooted_dirt", 33771, 33771},
	{"minecraft:mud", 33772, 33772},
	{"minecraft:deepslate", 33773, 33774},
	{"minecraft:cobbled_deepslate", 33776, 33776},
	{"minecraft:cobbled_deepslate_stairs", 33777, 33788},
	{"minecraft:cobbled_deepslate_slab", 33857, 33860},
	{"minecraft:cobbled_deepslate_wall", 33863, 33866},
	{"minecraft:polished_deepslate", 34187, 34187},
	{"minecraft:polished_deepslate_stairs", 34188, 34199},
	{"minecraft:polished_deepslate_slab", 34268, 34271},
	{"minecraft:polished_deepslate_wall", 34274, 34277},
	{"minecraft:deepslate_tiles", 34598, 34598},
	{"minecraft:deepslate_tile_stairs", 34599, 34610},
	{"minecraft:deepslate_tile_slab", 34679, 34682},
	{"minecraft:deepslate_tile_wall", 34685, 34688},
	{"minecraft:deepslate_bricks", 35009, 35009},
	{"minecraft:deepslate_brick_stairs", 35010, 35021},
	{"minecraft:deepslate_brick_slab", 35090, 35093},
	{"minecraft:deepslate_brick_wall", 35096, 35099},
	{"minecraft:chiseled_deepslate", 35420, 35420},
	{"minecraft:cracked_deepslate_bricks", 35421, 35421},
	{"minecraft:cracked_deepslate_tiles", 35422, 35422},
	{"minecraft:infested_deepslate", 35423, 35424},
	{"minecraft:smooth_basalt", 35426, 35426},
	{"minecraft:raw_iron_block", 35427, 35427},
	{"minecraft:raw_copper_block", 35428, 35428},
	{"minecraft:raw_gold_block", 35429, 35429},
	{"minecraft:potted_azalea_bush", 35430, 35430},
	{"minecraft:potted_flowering_azalea_bush", 35431, 35431},
	{"minecraft:ochre_froglight", 35432, 35433},
	{"minecraft:verdant_froglight", 35435, 35436},
	{"minecraft:pearlescent_froglight", 35438, 35439},
	{"minecraft:frogspawn", 35441, 35441},
	{"minecraft:reinforced_deepslate", 35442, 35442},
	{"minecraft:decorated_pot", 35443, 35452},
	{"minecraft:crafter", 35459, 35504},
	{"minecraft:trial_spawner", 35507, 35513},
	{"minecraft:vault", 35519, 35523},
	{"minecraft:heavy_core", 35551, 35552},
	{"minecraft:pale_moss_block", 35553, 35553},
	{"minecraft:pale_moss_carpet", 35554, 35554},
	{"minecraft:pale_hanging_moss", 35716, 35716},
	{"minecraft:open_eyeblossom", 35718, 35718},
	{"minecraft:closed_eyeblossom", 35719, 35719},
	{"minecraft:potted_open_eyeblossom", 35720, 35720},
	{"minecraft:potted_closed_eyeblossom", 35721, 35721},
	{"minecraft:firefly_bush", 35722, 35722},
}

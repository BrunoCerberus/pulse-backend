# Content Sources

Pulse ships **136 pre-configured sources** seeded across migrations
`001`/`003`/`008`/`009`/`010` (English, Portuguese, and Spanish feeds — articles,
podcasts, and YouTube channels). The group counts below sum to 136.

> **Configured vs. active:** 136 is the number of *seeded* rows. Migration `023`
> flips `is_active = false` on long-dead or never-producing feeds at apply time,
> so the number of *currently active* sources may be lower than 136. Query
> `GET /api-source-health` (see [api-reference.md](api-reference.md)) for the live
> active count.

Sources are stored in the `sources` table and can be edited in the Supabase
Dashboard — see [Adding new sources](#adding-new-sources) below.

## English (52)

### News Articles (14 sources)
| Source | Category |
|--------|----------|
| The Guardian (World, Tech, Business, Sport, Science) | Various |
| BBC News (World, Tech, Business, Health) | Various |
| NPR News | World |
| Ars Technica, TechCrunch, The Verge | Technology |
| Science Daily | Science |

### Podcasts (21 sources)
| Source | Topic |
|--------|-------|
| The Vergecast, ATP, Darknet Diaries | Technology |
| The Daily, Up First, Pod Save America | News & Politics |
| Radiolab, StarTalk, Science Vs | Science |
| Huberman Lab, Peter Attia, On Purpose | Health |
| Bill Simmons, Pardon My Take, Ringer NBA | Sports |
| How I Built This, Acquired, All-In | Business |
| SmartLess, Conan O'Brien, Armchair Expert | Entertainment |

### YouTube Channels (17 sources)
| Source | Topic |
|--------|-------|
| MKBHD, Fireship, Linus Tech Tips | Technology |
| Veritasium, Kurzgesagt, SmarterEveryDay | Science |
| Vox, PBS NewsHour | News |
| Doctor Mike, Jeff Nippard | Health |
| JomBoy Media, Secret Base | Sports |
| CNBC, Bloomberg | Business |
| First We Feast, Tonight Show, Hot Ones | Entertainment |

## Portuguese (43)

### Portuguese Articles (20 sources)
| Source | Category |
|--------|----------|
| Folha de S.Paulo, G1 (Globo), BBC Brasil, UOL Noticias | World |
| Tecnoblog, Olhar Digital, Canaltech | Technology |
| InfoMoney, Exame, Valor Economico | Business |
| ge (Globo Esporte), Gazeta Esportiva, UOL Esporte | Sports |
| G1 Ciencia e Saude, Revista Galileu, Superinteressante | Science |
| Veja Saude, Metropoles Saude | Health |
| CinePOP, PapelPop | Entertainment |

### Portuguese Podcasts (10 sources)
| Source | Topic |
|--------|-------|
| Braincast, Hipsters Ponto Tech, Tecnocast | Technology |
| Cafe da Manha | News |
| NerdCast, Flow Podcast | Entertainment |
| Naruhodo, Dragoes de Garagem | Science |
| PrimoCast | Business |
| Xadrez Verbal | Politics |

### Portuguese Videos (10 sources)
| Source | Topic |
|--------|-------|
| TecMundo, Filipe Deschamps | Technology |
| Manual do Mundo, Nerdologia | Science |
| BBC News Brasil | News |
| Desimpedidos | Sports |
| Porta dos Fundos | Entertainment |
| Me Poupe!, O Primo Rico | Business |
| Drauzio Varella | Health |

### Portuguese Politics (3 sources)
| Source | Category |
|--------|----------|
| Poder360, Congresso em Foco, Folha de S.Paulo Poder | Politics |

## Spanish (41)

### Spanish Articles (18 sources)
| Source | Category |
|--------|----------|
| El Pais, BBC Mundo, El Mundo, Infobae | World |
| Xataka, Hipertextual | Technology |
| Expansion, Cinco Dias, El Economista | Business |
| Marca, AS, Mundo Deportivo | Sports |
| Muy Interesante, National Geographic Espana | Science |
| 20 Minutos Salud | Health |
| SensaCine, Espinof, 20 Minutos Cine | Entertainment |

### Spanish Podcasts (10 sources)
| Source | Topic |
|--------|-------|
| Despeja la X | Technology |
| Radio Ambulante | News |
| Se Regalan Dudas, Nadie Sabe Nada, The Wild Project | Entertainment |
| TED en Espanol | Science |
| Entiende Tu Mente, Cristina Mitre | Health |
| El Partidazo de COPE | Sports |
| BBVA Blink | Business |

### Spanish Videos (10 sources)
| Source | Topic |
|--------|-------|
| Nate Gentile, Dot CSV | Technology |
| QuantumFracture, CdeCiencia | Science |
| BBC News Mundo, DW Espanol | News |
| Ibai, Luisito Comunica | Entertainment |
| Value School | Business |
| FisioOnline | Health |

### Spanish Politics (3 sources)
| Source | Category |
|--------|----------|
| elDiario.es Politica, La Vanguardia Politica, El Confidencial | Politics |

## Adding new sources

1. Go to Supabase Dashboard → **Table Editor** → **sources**
2. Click **Insert row**
3. Fill in:
   - `name`: Display name
   - `slug`: Unique identifier (lowercase, hyphens)
   - `feed_url`: RSS feed URL
   - `category_id`: Select from the `categories` table
   - `language`: ISO 639-1 code (e.g., `en`, `pt`, `es`) — defaults to `en`
   - `is_active`: `true`

New sources are picked up on the next scheduled fetch (every 2 hours) or a manual
`fetch-rss` run. For the SQL-based workflow and per-source tuning (e.g.
`max_content_length`, `fetch_interval_hours`), see
[operations-runbook.md](operations-runbook.md) and
[database-schema.md](database-schema.md).

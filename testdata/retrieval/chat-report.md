# Chat eval

2026-11-18, 80 questions of `queries.jsonl` asked of claude-haiku-5-5 at https://api.anthropic.com, a conversation each over the whole corpus (`make eval-chat`, docs/adr/0012-chat.md).

| category | questions | citations valid | section hit rate | strict hit rate | false refusals | correct refusals | unsourced | self-written markers | first token p50 / p95 | total p50 / p95 | mean cost |
|---|---|---|---|---|---|---|---|---|---|---|---|
| identifier | 15 | 100% (36/36) | 1.00 | 0.80 | 0.00 | – | 0 | 0 (0) | 1.8 s / 3.2 s | 3.1 s / 4.4 s | $0.00068 |
| error | 10 | 100% (23/23) | 0.80 | 0.70 | 0.00 | – | 1 | 1 (4) | 2.8 s / 4.0 s | 4.1 s / 5.5 s | $0.00080 |
| paraphrase | 20 | 100% (54/54) | 0.95 | 0.75 | 0.00 | – | 0 | 1 (3) | 2.4 s / 3.2 s | 3.2 s / 4.8 s | $0.00087 |
| howto | 17 | 100% (45/45) | 1.00 | 0.53 | 0.00 | – | 0 | 0 (0) | 1.8 s / 3.0 s | 3.0 s / 4.3 s | $0.00076 |
| multidoc | 10 | 100% (44/44) | 1.00 | 0.60 | 0.00 | – | 0 | 0 (0) | 2.2 s / 3.9 s | 3.7 s / 5.4 s | $0.00097 |
| none | 8 | 100% (2/2) | – | – | – | 0.50 | 3 | 0 (0) | 0.0 s / 2.0 s | 2.8 s / 3.6 s | $0.00139 |
| all | 80 | 100% (204/204) | 0.96 | 0.68 | 0.00 | 0.50 | 4 | 2 (7) | 2.0 s / 3.3 s | 3.2 s / 5.0 s | $0.00087 |

Both hit rates count answerable questions. **Section** (the headline): an answer cites a block under the same headings, in the same document, as an expected block, or under a subsection of them. **Strict**: an answer cites an expected block itself. The labels name one block per answer where the model may cite its neighbours, so strict undercounts. A refusal is the model's own signal: it called `not_in_library`, and the message is `not_found`; a false refusal is one on an answerable question, a correct one on a question with no answer. **Unsourced** counts answers with text and no valid citation, which the app shows as "No sources". **Self-written markers** are citation markers such as [1] or [2-3] the model wrote into its text outside code, as answers with markers (markers in all). Every answer, with what it cited, is in `chat-answers.jsonl`. In all: 17 tool rounds, 0 citations dropped as invalid, 0 answers not done; 629035 input tokens, 31% read from cache; the largest question read 20951 input tokens over all its calls; total cost $0.0693.

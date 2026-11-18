# Chat eval

2026-11-18, 80 questions of `queries.jsonl` asked of claude-haiku-5-5 at https://api.anthropic.com, a conversation each over the whole corpus (`make eval-chat`, docs/adr/0012-chat.md).

| category | questions | citations valid | section hit rate | strict hit rate | false refusals | correct refusals | first token p50 / p95 | total p50 / p95 | mean cost |
|---|---|---|---|---|---|---|---|---|---|
| identifier | 15 | 100% (39/39) | 1.00 | 0.80 | 0.00 | – | 2.6 s / 4.2 s | 3.6 s / 5.3 s | $0.00075 |
| error | 10 | 100% (21/21) | 0.90 | 0.60 | 0.10 | – | 2.3 s / 4.4 s | 3.9 s / 5.4 s | $0.00087 |
| paraphrase | 20 | 100% (58/58) | 0.90 | 0.75 | 0.00 | – | 2.7 s / 3.9 s | 3.7 s / 5.2 s | $0.00088 |
| howto | 17 | 100% (52/52) | 1.00 | 0.53 | 0.00 | – | 2.4 s / 3.9 s | 3.7 s / 5.3 s | $0.00078 |
| multidoc | 10 | 100% (47/47) | 1.00 | 0.70 | 0.00 | – | 3.0 s / 4.2 s | 4.9 s / 5.6 s | $0.00099 |
| none | 8 | 100% (2/2) | – | – | – | 0.75 | 2.2 s / 4.0 s | 3.3 s / 5.0 s | $0.00148 |
| all | 80 | 100% (219/219) | 0.96 | 0.68 | 0.01 | 0.75 | 2.6 s / 4.0 s | 3.8 s / 5.4 s | $0.00090 |

Both hit rates count answerable questions. **Section** (the headline): an answer cites a block under the same headings, in the same document, as an expected block. **Strict**: an answer cites an expected block itself. The labels name one block per answer where the model may cite its neighbours, so strict undercounts. A refusal is an answer with no valid citation, shown as "Not found in your library". Every answer, with what it cited, is in `chat-answers.jsonl`. In all: 17 tool rounds, 0 citations dropped as invalid, 0 answers not done; 604258 input tokens, 28% read from cache; the largest question read 20003 input tokens over all its calls; total cost $0.0724.

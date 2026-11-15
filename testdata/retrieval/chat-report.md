# Chat eval

2026-11-15, 80 questions of `queries.jsonl` asked of claude-haiku-5-5 at https://api.anthropic.com, a conversation each over the whole corpus (`make eval-chat`, docs/adr/0012-chat.md).

| category | questions | citations valid | hit rate | false refusals | correct refusals | first token p50 / p95 | total p50 / p95 | mean cost |
|---|---|---|---|---|---|---|---|---|
| identifier | 15 | 100% (42/42) | 0.73 | 0.00 | – | 2.6 s / 4.5 s | 4.2 s / 7.0 s | $0.00078 |
| error | 10 | 100% (24/24) | 0.70 | 0.00 | – | 2.4 s / 5.3 s | 3.6 s / 6.5 s | $0.00087 |
| paraphrase | 20 | 100% (55/55) | 0.70 | 0.00 | – | 3.1 s / 3.9 s | 4.3 s / 5.5 s | $0.00086 |
| howto | 17 | 100% (49/49) | 0.47 | 0.00 | – | 3.1 s / 4.8 s | 4.1 s / 7.8 s | $0.00083 |
| multidoc | 10 | 100% (44/44) | 0.60 | 0.00 | – | 3.2 s / 7.4 s | 5.1 s / 7.4 s | $0.00104 |
| none | 8 | 100% (2/2) | – | – | 0.88 | 2.6 s / 6.3 s | 3.7 s / 7.9 s | $0.00148 |
| all | 80 | 100% (216/216) | 0.64 | 0.00 | 0.88 | 2.9 s / 4.8 s | 4.2 s / 7.0 s | $0.00092 |

Hit rate counts answerable questions with a cited block among those expected; a refusal is an answer with no valid citation, shown as "Not found in your library". In all: 18 tool rounds, 0 citations dropped as invalid, 0 answers not done; 617260 input tokens, 28% read from cache; the largest question read 20558 input tokens over all its calls; total cost $0.0740.

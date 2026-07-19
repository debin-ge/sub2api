# Third-Party Data Notices

## LMArena leaderboard data

Model Radar uses the public `text_style_control` leaderboard published in the
[LMArena leaderboard dataset](https://huggingface.co/datasets/lmarena-ai/leaderboard-dataset)
by `lmarena-ai`.

The dataset is licensed under
[Creative Commons Attribution 4.0 International](https://creativecommons.org/licenses/by/4.0/)
(CC BY 4.0). Sub2API filters the complete `overall` leaderboard against the
public model catalog using deterministic model-ID normalization, orders matches
by the original LMArena rank, and publishes at most ten matching rows. These
filtering and matching changes are made by the Sub2API project and are not
endorsed by LMArena.

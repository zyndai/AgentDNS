The Complete 5-Step Framework

Step 1: Self-Consistency Sampling
What it is: Ask the same question to your agent multiple times and study the pattern of answers.

Why this step is needed:
LLMs are not like calculators. They do not give the same answer every time. A single answer tells you almost nothing reliable. But if you ask 10 times, the frequency pattern reveals the agent's true confidence

Ask once:
Q: "What does RSI above 70 mean?"
A: "Overbought condition"
→ One answer. Is this reliable? You have no idea.

Ask 10 times:
"Overbought condition"   → 7 times  (Rank 1)
"Bullish momentum"       → 2 times  (Rank 2)
"Buy signal"             → 1 time   (Rank 3)
→ Now you see: agent is 70% confident about "Overbought"


Why it is important:

High consistency (8-10/10 same answer) → Agent knows this topic well
Medium consistency (5-7/10) → Agent is somewhat uncertain
Low consistency (1-4/10) → Agent is guessing
Consistent but wrong → Agent has systematic bias (dangerous)

What it produces: A ranked list of answers ordered by frequency. This becomes the raw material for everything that follows.
Technical note: Temperature must be set above 0 (recommended: 0.7) to allow variation between samples. Temperature 0 gives the same answer every time, which defeats the purpose.



Step 2: Nonconformity Scores

What it is: Convert the frequency rankings from Step 1 into actual numbers that measure how unusual or surprising the correct answer is.

Why this step is needed:

Rankings like "Rank 1, Rank 2, Rank 3" are labels. The mathematical framework needs numbers to calculate thresholds and guarantees. Nonconformity scores are that conversion.
The rule is simple:

Nonconformity Score = Rank of the correct answer
                      in the frequency ranking

Correct answer is Rank 1 → Score = 1  (LOW — good)
Correct answer is Rank 2 → Score = 2  (MEDIUM)
Correct answer is Rank 3 → Score = 3  (HIGH — bad)
Correct answer is Rank 4 → Score = 4  (VERY HIGH — terrible)

Q: "What is a death cross?"
Correct answer: "Bearish signal"

Agent sampled 10 times:
"Bullish reversal"  → 6 times → Rank 1  (WRONG)
"Bearish signal"    → 3 times → Rank 2  (CORRECT)
"Neutral pattern"   → 1 time  → Rank 3

Correct answer is Rank 2
Nonconformity Score = 2
→ Agent ranked a wrong answer higher than the correct one

Why it is important:

Low scores mean the agent consistently puts correct answers first — trustworthy
High scores mean the agent buries correct answers — unreliable
Consistent high scores reveal systematic bias that sampling alone would not catch

What it produces: A list of 50 scores — one per calibration question — like [1, 2, 1, 4, 1, 3, 2, 1, 1, 2...]. This list feeds directly into Step 3.



Step 3: Calibration

What it is: Take the 50 nonconformity scores from Step 2 and find the threshold number that covers your target reliability (e.g., 95% of scores fall below this threshold).

Why this step is needed:
You now have 50 numbers. Calibration answers: what do you DO with them? It finds the one threshold value that will make prediction sets reliable at your target level.

Step by step process:
Your 50 scores:
[1,2,1,4,1,3,2,1,1,2,1,1,2,1,1,
 3,1,1,2,1,1,1,4,1,2,1,1,1,3,1...]

Step 1: Sort low to high
[1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,
 1,1,1,1,1,2,2,2,2,2,2,2,2,2,2,
 3,3,3,3,3,4,4...]

Step 2: Find 95th percentile (position 48 of 50)
→ Value at position 48 = 3

Step 3: Threshold = 3

How the threshold builds prediction sets:
New question comes in. Agent is sampled. Scores are calculated.

Score ≤ 3 → Include in prediction set 
Score > 3 → Exclude from prediction set 

Example
"RSI above 70"    Score 1 → INCLUDE 
"Bollinger Band"  Score 2 → INCLUDE 
"MACD signal"     Score 3 → INCLUDE 
"Random answer"   Score 5 → EXCLUDE 

Prediction Set = {RSI above 70, Bollinger Band, MACD signal}

What prediction set size tells you:

**Why it is important:**

Calibration is where raw numbers become actionable decisions. It is the mathematical heart of the framework — the moment where statistical theory meets real deployment decisions.

**The Finite-Sample Guarantee kicks in here:**

With just 50 calibration examples, the error in your threshold is at most `1/(50+1) = 1.96%`. This means your guarantee is mathematically valid with only 50 examples — no need for thousands of labeled samples.


Step 4: Coverage Guarantee

**What it is:** Run the agent on the 50 unseen test questions, build prediction sets using the threshold from Step 3, and check how often the correct answer is inside the set

**Why this step is needed:**

Calibration set the threshold. But does it actually work on new, unseen questions? The test set answers that question and produces your final reliability percentage.

**Step by step process:**

```
Test Q1: "What does MACD crossover indicate?"
Correct answer: "Bullish momentum"
Prediction set: {Bullish momentum, Trend reversal, Buy signal}
Correct answer inside set? YES ✅

Test Q2: "What is dollar cost averaging?"
Correct answer: "Investing fixed amount regularly"
Prediction set: {Investing fixed amount regularly, Buying dips only}
Correct answer inside set? YES ✅

Test Q3: "What is a circuit breaker?"
Correct answer: "Trading halt mechanism"
Prediction set: {Price limit, Volatility control, Index reset}
Correct answer inside set? NO ❌

...continue for all 50 test questions...

Final count: 47 out of 50 correct answers found inside prediction set
```

**Calculate Coverage Guarantee:**

```
Coverage = correct answers found in set / total test questions

Coverage = 47 / 50 = 94%
```

This 94% is not just an accuracy score from testing. It is a **mathematically proven guarantee** that holds on any new question — because of conformal prediction's distribution-free, finite-sample properties.

**Why this step is needed:**

Calibration set the threshold. But does it actually work on new, unseen questions? The test set answers that question and produces your final reliability percentage.

**Step by step process:**

```
Test Q1: "What does MACD crossover indicate?"
Correct answer: "Bullish momentum"
Prediction set: {Bullish momentum, Trend reversal, Buy signal}
Correct answer inside set? YES ✅

Test Q2: "What is dollar cost averaging?"
Correct answer: "Investing fixed amount regularly"
Prediction set: {Investing fixed amount regularly, Buying dips only}
Correct answer inside set? YES ✅

Test Q3: "What is a circuit breaker?"
Correct answer: "Trading halt mechanism"
Prediction set: {Price limit, Volatility control, Index reset}
Correct answer inside set? NO ❌

...continue for all 50 test questions...

Final count: 47 out of 50 correct answers found inside prediction set
```

**Calculate Coverage Guarantee:**

```
Coverage = correct answers found in set / total test questions

Coverage = 47 / 50 = 94%
```

This 94% is not just an accuracy score from testing. It is a **mathematically proven guarantee** that holds on any new question — because of conformal prediction's distribution-free, finite-sample properties.


Ranking 5 Trading Agents

Run the exact same 50 calibration + 50 test questions on all agents. Same threshold target. Same evaluation. Then compare:

Agent   Coverage Avg_Set_Size  Rank?
Agent_A   94%      1.2YES        1
Agent_B   91%      1.8YES        2
Agent_C   87%      2.4NO         3
Agent_D   76%      3.1NO         4
Agent_E   64%      4.2NO         5

## Why This is Better Than LLM-as-Judge

LLM-as-Judge sounds like the easy solution: use another LLM to evaluate your agent's answers. No human needed, fully automated. But it has serious, proven problems.
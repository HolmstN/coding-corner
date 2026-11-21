# Coding Corner

Practical coding challenges to work through, share, and discuss.

## Challenges

- [TCP Gateway](challenges/tcp-gateway/README.md): build a gateway that routes
  framed TCP requests to backend services.

## Submitting a solution

Open a PR with your solution in `submissions/<challenge>/<name>/`. Include a short
README explaining how to run it, how you handled concurrency, and what you would
change at 10x the traffic.

Each challenge defines its own acceptance tests and launch contract. PR checks
run the TCP Gateway suite for every added or changed TCP Gateway submission.
Don't change the shared tests to make your submission pass.

Then we'll get together and walk through the code. Explaining your choices is
part of the challenge.

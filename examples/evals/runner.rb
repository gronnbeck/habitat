# frozen_string_literal: true

# A runner declares the agents under evaluation and hands off. That is the
# whole of it: no suite parsing, no assertions, no reporting.
#
# Run it through the engine, never directly:
#
#   habitat run echo --dir examples

$LOAD_PATH.unshift(File.expand_path("../../sdk/ruby/lib", __dir__))

require "habitat"

# Optional. Only needed if you want habitat to derive cost from token counts;
# you supply the prices, so nothing here assumes a particular provider.
Habitat.configure do |c|
  c.price "pretend-model-1", input: 3.0, output: 15.0
end

# A stand-in for real application code, so this example costs nothing to run.
Habitat.target "echo_agent" do |input:, context:|
  message = input[:message].to_s

  Habitat::Result.new(
    output: "you said: #{message}",
    # Structured fields are what state_match grades. Prose belongs in output.
    final_state: {
      echoed: message,
      length: message.length,
      repetition: context[:repetition_index]
    },
    usage: Habitat::Usage.from_tokens(model: "pretend-model-1", input: 12, output: 8)
  )
end

Habitat.start

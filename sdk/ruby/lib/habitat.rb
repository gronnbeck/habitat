# frozen_string_literal: true

require_relative "habitat/version"

module Habitat
  class Error < StandardError; end
end

require_relative "habitat/usage"
require_relative "habitat/result"
require_relative "habitat/registry"
require_relative "habitat/client"
require_relative "habitat/runner"

# Habitat is an evaluation engine for code whose output cannot be asserted
# byte-for-byte. This gem is its Ruby SDK.
#
# Your part is small on purpose: name your agents and say how to run them,
# then hand off.
#
#   require "habitat"
#
#   Habitat.configure do |c|
#     c.price "claude-opus-5", input: 5.0, output: 25.0   # optional, USD per 1M tokens
#   end
#
#   Habitat.target "coach_chat" do |input:, context:|
#     reply = CoachChatResponder.call(...)
#
#     Habitat::Result.new(
#       output: reply[:text],
#       final_state: { proposed: reply[:proposal].present? },
#       usage: Habitat::Usage.from_tokens(model: "claude-opus-5", input: 1200, output: 380)
#     )
#   end
#
#   Habitat.start
#
# Everything after `start` — fetching the plan, repetitions, timing, posting
# results, error isolation — belongs to the runner. This SDK never parses a
# suite file and never decides whether anything passed.
module Habitat
  # Project-level settings. Only cost pricing so far, and even that is
  # optional: report `cost_usd` directly, or report nothing at all.
  class Config
    def initialize
      @prices = {}
    end

    # Registers what a model costs, in USD per million tokens. Habitat ships
    # no price table — you supply these, so nothing here assumes a provider.
    def price(model, input:, output:)
      @prices[model.to_s] = Usage::Price.new(input:, output:)
    end

    def price_for(model) = @prices[model.to_s]
  end

  class << self
    def config = @config ||= Config.new

    def configure
      yield config
      config
    end

    def registry = @registry ||= Registry.new

    # Declares an agent: a name, and how to run it.
    def target(name, callable = nil, &block)
      registry.register(name, callable, &block)
    end

    # Hands off to the runner. Blocks until every execution has been reported.
    def start(client: nil)
      runner = Runner.new(registry:, client: client || Client.new)
      runner.call
    end

    # Test seam: forget all registered targets and configuration.
    def reset!
      @registry = nil
      @config = nil
    end
  end
end

# frozen_string_literal: true

module Habitat
  # What one execution consumed. Every field is optional.
  #
  # Cost is reported by the agent, never computed by the engine — habitat has
  # no idea which provider served a model or what it charges, and guessing
  # would be worse than saying nothing. When no execution in a run reports a
  # cost, reports omit cost entirely rather than showing a misleading $0.00.
  #
  # Three ways to report it, in increasing order of laziness:
  #
  #   # 1. You already know the cost.
  #   Habitat::Usage.new(cost_usd: 0.0123)
  #
  #   # 2. You know the tokens, and told habitat what they cost.
  #   Habitat.configure { |c| c.price("claude-opus-5", input: 5.0, output: 25.0) }
  #   Habitat::Usage.from_tokens(model: "claude-opus-5", input: 1_200, output: 380)
  #
  #   # 3. You don't care. Pass no usage at all.
  class Usage
    attr_reader :cost_usd, :input_tokens, :output_tokens

    def initialize(cost_usd: nil, input_tokens: nil, output_tokens: nil)
      @cost_usd = cost_usd
      @input_tokens = input_tokens
      @output_tokens = output_tokens
    end

    # Derives cost from a token count using a price registered with
    # Habitat.configure. Prices are supplied by you, per million tokens, so
    # this stays provider-agnostic: habitat ships no price table of its own.
    #
    # An unregistered model is not an error — you get the token counts with no
    # cost attached, which is exactly what habitat does with a missing cost.
    def self.from_tokens(model:, input: nil, output: nil)
      price = Habitat.config.price_for(model)
      new(cost_usd: price&.cost(input:, output:), input_tokens: input, output_tokens: output)
    end

    # Reads an Anthropic-style usage object or hash, which is the shape most
    # SDKs expose. Nothing here is provider-specific beyond the two key names.
    def self.from_response(usage, model: nil)
      return nil if usage.nil?

      input = read(usage, :input_tokens)
      output = read(usage, :output_tokens)
      return new(input_tokens: input, output_tokens: output) if model.nil?

      from_tokens(model:, input:, output:)
    end

    def self.read(usage, key)
      return usage[key] || usage[key.to_s] if usage.respond_to?(:[]) && !usage.is_a?(Struct)

      usage.respond_to?(key) ? usage.public_send(key) : nil
    end
    private_class_method :read

    def to_h
      { cost_usd:, input_tokens:, output_tokens: }.compact
    end

    # A model's price, in USD per million tokens.
    class Price
      def initialize(input:, output:)
        @input = input
        @output = output
      end

      def cost(input: nil, output: nil)
        return nil if input.nil? && output.nil?

        ((input.to_i * @input) + (output.to_i * @output)) / 1_000_000.0
      end
    end
  end
end

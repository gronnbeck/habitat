# frozen_string_literal: true

require "json"
require "net/http"
require "uri"

module Habitat
  # The HTTP side of the handoff: fetch the plan, post results back.
  #
  # Deliberately the only place in the SDK that knows the wire format exists.
  # It speaks to whichever habitat process launched this runner, addressed by
  # HABITAT_URL, and never parses a suite file or evaluates an expectation.
  class Client
    class TransportError < Habitat::Error; end

    PROTOCOL = "v1"

    def initialize(url: ENV.fetch("HABITAT_URL", nil), token: ENV.fetch("HABITAT_RUN_TOKEN", nil))
      raise TransportError, "HABITAT_URL is not set — run this through `habitat run`" if url.nil? || url.empty?

      @base = URI.parse(url)
      @token = token
    end

    # The executions this runner should perform.
    def plan
      get("/#{PROTOCOL}/plan")
    end

    # Reports one execution's result.
    def submit(case_id:, repetition_index:, result:)
      post("/#{PROTOCOL}/executions", {
        case_id:, repetition_index:, result: result.to_h
      })
    end

    # Signals that the runner is finished. The engine also notices the process
    # exiting, so this is a courtesy that lets grading start a moment sooner.
    def complete
      post("/#{PROTOCOL}/complete", {})
    rescue TransportError
      # Never let the goodbye be what fails a run that already reported.
      nil
    end

    private

    def get(path)
      request(Net::HTTP::Get.new(uri_for(path)))
    end

    def post(path, payload)
      request = Net::HTTP::Post.new(uri_for(path))
      request["Content-Type"] = "application/json"
      request.body = JSON.generate(payload)
      request(request)
    end

    def uri_for(path) = URI.join(@base, path)

    def request(request)
      request["Authorization"] = "Bearer #{@token}" if @token
      response = Net::HTTP.start(@base.host, @base.port, read_timeout: 600) do |http|
        http.request(request)
      end
      handle(response)
    rescue SystemCallError, IOError, Net::OpenTimeout, Net::ReadTimeout => e
      raise TransportError, "could not reach the habitat engine at #{@base}: #{e.message}"
    end

    def handle(response)
      unless response.is_a?(Net::HTTPSuccess)
        raise TransportError, "habitat engine returned #{response.code}: #{response.body}"
      end

      return nil if response.body.nil? || response.body.empty?

      JSON.parse(response.body)
    end
  end
end

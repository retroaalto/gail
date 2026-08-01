# frozen_string_literal: true

class Gail < Formula
  desc "Tail -f viewer for the terminal"
  homepage "https://github.com/retroaalto/gail"
  url "https://github.com/retroaalto/gail/archive/refs/tags/v0.4.tar.gz"
  sha256 "b9e3dc72bc430015e99d91ad1fe9cac1aa2f9faf465aef3bba1e0088ede18b87"
  license "MIT"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.Version=#{version}"
    system "go", "build", *std_go_args(ldflags:)
  end

  test do
    system "#{bin}/gail", "--version"
  end
end

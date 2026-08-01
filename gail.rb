# frozen_string_literal: true

class Gail < Formula
  desc "Tail -f viewer for the terminal"
  homepage "https://github.com/retroaalto/gail"
  url "https://github.com/retroaalto/gail/archive/refs/tags/v0.2.tar.gz"
  sha256 "3129d576ab71120ddbe811b6d6b308967af802fa9414bf99803d6335f7509c39"
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

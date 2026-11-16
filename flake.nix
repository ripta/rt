{
  description = "rt: Ripta's collection of tools";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        default = rt;

        # The all-in-one hyperbinary, equivalent to:
        #   go install github.com/ripta/rt/hypercmd/rt@latest
        # go.mod requires go1.27, newer than nixpkgs' default go.
        rt = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
          pname = "rt";
          version = self.shortRev or self.dirtyShortRev or "unknown";
          src = self;

          vendorHash = "sha256-kAEplslUsD360h3uM5uDJt3g3L4CU6/dNP0OCHjwHFs=";

          subPackages = [ "hypercmd/rt" ];

          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X github.com/ripta/rt/pkg/version.Version=${self.shortRev or self.dirtyShortRev or "unknown"}"
          ];

          # Tests run in CI; the hyperbinary package itself has none.
          doCheck = false;

          meta = {
            description = "Ripta's collection of tools";
            homepage = "https://github.com/ripta/rt";
            license = pkgs.lib.licenses.mit;
            mainProgram = "rt";
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_27
            gotools # goimports
            protobuf # protoc
            protoc-gen-go
          ];
        };
      });
    };
}

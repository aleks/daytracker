{ ... }:

{
  languages.go.enable = true;
  languages.typescript.enable = true;
  languages.nix.enable = true;

  processes = {
    web = {
      exec = "make dev-web";
      watch = {
        paths = [ ./web/src ./web/public ./web/package.json];
      };
    };

    api = {
      exec = "make dev-api";
      watch = {
        paths = [ ./cmd ./internal ];
        extensions = [ "go" ];
        ignore = [ "*_test.go" ];
      };
      after = [ "devenv:processes:web@started" ];
    };
  };

  scripts.build.exec = "make build";
  scripts.test.exec = "make test";
  scripts.check.exec = "make check";

  enterShell = ''
    echo
    echo "  devenv up       — start development server"
    echo "  make build      — build server + frontend"
    echo "  make test       — run all tests"
    echo "  make check      — connector health check"
    echo
  '';
}

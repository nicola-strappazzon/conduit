Output assets/demo.gif

Require echo
Require tmux

Set Shell "bash"
Set FontFamily "Hack Nerd Font"
Set FontSize 16
Set Width 1420
Set Height 700
Set TypingSpeed 20ms

Hide
Type "tmux new-session -d -s conduit-demo 'bash --noprofile --rcfile docs/demo.bashrc -i'" Enter
Type "tmux set-option -t conduit-demo status off" Enter
Type "tmux split-window -v -b -l 12 -t conduit-demo 'bash --noprofile --rcfile docs/demo.bashrc -i'" Enter
Type "tmux select-pane -t conduit-demo:0.0" Enter
Type "tmux attach -t conduit-demo" Enter
Sleep 1s
Ctrl+B
Type ":resize-pane -t conduit-demo:0.0 -y 12" Enter
Sleep 500ms
Show
Sleep 1s
Type "conduit --profile $PROFILE --target i-0d89d20fd1db52703 --chrome-profile $CHROME --local-port 3306 --remote-port 3306" Enter
Sleep 3s
Ctrl+B
Type "o"
Sleep 500ms
Type "mysql -h 127.0.0.1 -P 3306 -p$MYSQL_PWD -u $MYSQL_USER" Enter
Sleep 8s
Hide
Ctrl+B
Type ":kill-session" Enter
Sleep 1s

# the chat ID of the group the bot will send messages.
# only used for sending non-reply messages
export GROUP_ID=

# name of the base file containing the messages
# for file messages/user.txt, BASENAME=user
export BASENAME=

# username of your bot, for replying to mentions
export BOT_USERNAME=

# this makes the bot reply pretending to be @{REAL_USERNAME}
export REAL_USERNAME=

# telegram bot token
export TOKEN=

go run -race main.go